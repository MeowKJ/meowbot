package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kong-jing/meowbot/modules/interest-radar/internal/model"
)

type Client struct {
	baseURL string
	apiKey  string
	model   string
}

func New(baseURL, apiKey, model string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, model: model}
}

func (c *Client) Enabled() bool { return c != nil && c.apiKey != "" && c.model != "" }

func (c *Client) Summarize(ctx context.Context, it model.Item, ruleReason string) (string, string, error) {
	summary, reason, _, err := c.Enrich(ctx, it, ruleReason, nil)
	return summary, reason, err
}

func (c *Client) Enrich(ctx context.Context, it model.Item, ruleReason string, ruleTags []string) (string, string, []string, error) {
	if !c.Enabled() {
		return "", "", nil, nil
	}
	prompt := "请用中文给这个个人技术雷达候选写一个不超过80字摘要、一个不超过80字推荐理由，并给出1-5个分类标签。只返回JSON: {\"summary\":\"...\",\"reason\":\"...\",\"tags\":[\"esp32\",\"tinyml\"]}\n标题:" + it.Title + "\n内容:" + it.Content + "\n规则理由:" + ruleReason + "\n规则标签:" + strings.Join(ruleTags, ", ")
	body := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": "你是克制的技术雷达摘要器，只关注可复现、可部署、可玩的价值。"},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.2,
	}
	content, err := c.chat(ctx, body)
	if err != nil {
		return "", "", nil, err
	}
	var parsed struct {
		Summary string   `json:"summary"`
		Reason  string   `json:"reason"`
		Tags    []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(jsonPayload(content)), &parsed); err != nil {
		return "", "", nil, nil
	}
	return parsed.Summary, parsed.Reason, cleanTags(parsed.Tags), nil
}

func (c *Client) Plan(ctx context.Context, topics []model.Topic, now time.Time) ([]model.ExplorationTask, error) {
	if !c.Enabled() {
		return nil, nil
	}
	type topicView struct {
		Name     string   `json:"name"`
		Weight   float64  `json:"weight"`
		Source   string   `json:"source"`
		Keywords []string `json:"keywords,omitempty"`
	}
	var active []topicView
	var blocked []topicView
	for _, t := range topics {
		view := topicView{Name: t.Name, Weight: t.Weight, Source: t.Source, Keywords: t.Keywords}
		if t.Source == "blocked" || t.Weight < 0 {
			blocked = append(blocked, view)
			continue
		}
		if t.Weight > 0 {
			active = append(active, view)
		}
		if len(active) >= 14 && len(blocked) >= 8 {
			break
		}
	}
	payload := map[string]any{
		"today":          now.Format("2006-01-02"),
		"active_topics":  active,
		"blocked_topics": blocked,
		"allowed_tools":  []string{"github_search", "arxiv", "rss"},
		"goal":           "为个人技术雷达生成 5-8 个搜索/探索任务，偏向可复现、有代码、有 demo、硬件/机器人/边缘 AI/论文与开源项目交叉，避开商业营销、套壳产品、Web3。query 必须是短关键词短语，适合 GitHub Repository Search 和 arXiv all 字段；不要使用 site:、括号、OR、AND、引号、URL 或网页搜索语法。",
	}
	b, _ := json.Marshal(payload)
	prompt := "你是个人网络雷达的 Source Planner。只返回 JSON，不要 Markdown。JSON schema: {\"tasks\":[{\"query\":\"string\",\"reason\":\"string\",\"tools\":[\"github_search\"|\"arxiv\"|\"rss\"],\"ttl_days\":7}]}\n输入:\n" + string(b)
	body := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": "你只输出可被机器解析的 JSON。搜索任务要具体、短、可执行，不要编造结果。"},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.25,
		"response_format": map[string]string{
			"type": "json_object",
		},
	}
	content, err := c.chat(ctx, body)
	if err != nil {
		delete(body, "response_format")
		content, err = c.chat(ctx, body)
		if err != nil {
			return nil, err
		}
	}
	return parsePlanJSON(content, now)
}

func (c *Client) chat(ctx context.Context, body map[string]any) (string, error) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("llm %s", res.Status)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", nil
	}
	return out.Choices[0].Message.Content, nil
}

func parsePlanJSON(content string, now time.Time) ([]model.ExplorationTask, error) {
	var parsed struct {
		Tasks []struct {
			Query   string   `json:"query"`
			Reason  string   `json:"reason"`
			Tools   []string `json:"tools"`
			TTLDays int      `json:"ttl_days"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(jsonPayload(content)), &parsed); err != nil {
		return nil, err
	}
	var out []model.ExplorationTask
	for _, raw := range parsed.Tasks {
		query := cleanQuery(raw.Query)
		reason := strings.Join(strings.Fields(raw.Reason), " ")
		tools := allowedTools(raw.Tools)
		if query == "" || reason == "" || len(tools) == 0 {
			continue
		}
		ttl := raw.TTLDays
		if ttl <= 0 {
			ttl = 7
		}
		if ttl > 14 {
			ttl = 14
		}
		out = append(out, model.ExplorationTask{
			Query:     query,
			Reason:    "AI JSON planner: " + reason,
			Tools:     tools,
			Status:    "planned",
			CreatedAt: now,
			ExpiresAt: now.AddDate(0, 0, ttl),
		})
		if len(out) >= 8 {
			break
		}
	}
	return out, nil
}

func cleanQuery(raw string) string {
	raw = strings.ReplaceAll(raw, "\"", " ")
	raw = strings.ReplaceAll(raw, "'", " ")
	raw = strings.ReplaceAll(raw, "(", " ")
	raw = strings.ReplaceAll(raw, ")", " ")
	parts := strings.Fields(raw)
	out := parts[:0]
	for _, part := range parts {
		lower := strings.ToLower(part)
		if lower == "or" || lower == "and" || strings.HasPrefix(lower, "site:") || strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
			continue
		}
		out = append(out, part)
		if len(out) >= 10 {
			break
		}
	}
	return strings.Join(out, " ")
}

func jsonPayload(content string) string {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	return strings.TrimSpace(content)
}

func allowedTools(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, tool := range in {
		tool = strings.TrimSpace(tool)
		switch tool {
		case "github_search", "arxiv", "rss":
			if !seen[tool] {
				out = append(out, tool)
				seen[tool] = true
			}
		}
	}
	return out
}

func cleanTags(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, tag := range in {
		tag = strings.ToLower(strings.TrimSpace(tag))
		tag = strings.ReplaceAll(tag, " ", "_")
		tag = strings.Trim(tag, "_-/")
		if tag == "" || len([]rune(tag)) > 40 || seen[tag] {
			continue
		}
		out = append(out, tag)
		seen[tag] = true
		if len(out) >= 5 {
			break
		}
	}
	return out
}

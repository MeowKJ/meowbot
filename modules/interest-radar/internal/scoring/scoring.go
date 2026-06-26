package scoring

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/kong-jing/meowbot/modules/interest-radar/internal/model"
)

type Result struct {
	Score  float64
	Tags   []string
	Reason string
	Status string
}

var wordRE = regexp.MustCompile(`[a-zA-Z0-9][a-zA-Z0-9_\-+.]{1,}`)

func Score(it model.Item, topics []model.Topic) Result {
	text := strings.ToLower(it.Title + " " + it.Content + " " + it.Source)
	var tags []string
	relevance := 0.0
	blocked := false
	for _, topic := range topics {
		if topic.ExpiresAt != nil {
			continue
		}
		hit := false
		for _, kw := range append(topic.Keywords, topic.Name) {
			if kw != "" && strings.Contains(text, strings.ToLower(kw)) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		tags = append(tags, topic.Name)
		if topic.Source == "blocked" || topic.Weight < 0 {
			blocked = true
			relevance -= math.Abs(topic.Weight) + 0.8
		} else {
			relevance += topic.Weight
		}
	}
	if relevance > 1 {
		relevance = 1
	}
	if relevance < 0 {
		relevance = 0
	}
	playability := boolScore(text, []string{"github", "code", "demo", "example", "tutorial", "build", "weekend", "open source", "firmware"})
	implementability := boolScore(text, []string{"repo", "schematic", "bom", "instructions", "documentation", "library", "sdk"})
	novelty := boolScore(text, []string{"new", "release", "latest", "recent", "2026", "v0.", "v1.", "paper"})
	hardware := boolScore(text, []string{"esp32", "risc-v", "pcb", "sensor", "robot", "hardware", "tinyml", "camera", "arduino"})
	final := relevance*0.35 + playability*0.25 + implementability*0.20 + novelty*0.10 + hardware*0.10
	status := "candidate"
	if blocked || final < 0.18 || strings.Contains(text, "airdrop") || strings.Contains(text, "web3") || strings.Contains(text, "crypto") {
		status = "ignored"
	}
	sort.Strings(tags)
	tags = compact(tags)
	reason := Explain(tags, playability, implementability, novelty, hardware)
	return Result{Score: final, Tags: tags, Reason: reason, Status: status}
}

func Explain(tags []string, playability, implementability, novelty, hardware float64) string {
	parts := []string{}
	if len(tags) > 0 {
		parts = append(parts, "命中 "+strings.Join(tags, " / "))
	}
	if playability > 0 {
		parts = append(parts, "看起来有代码、demo 或周末可玩价值")
	}
	if implementability > 0 {
		parts = append(parts, "实现资料相对完整")
	}
	if novelty > 0 {
		parts = append(parts, "近期更新或新发布")
	}
	if hardware > 0 {
		parts = append(parts, "包含硬件/边缘设备因素")
	}
	if len(parts) == 0 {
		return "规则评分通过，但暂未命中强解释信号"
	}
	return strings.Join(parts, "；")
}

func Summary(it model.Item) string {
	txt := strings.TrimSpace(it.Content)
	if txt == "" {
		txt = it.Title
	}
	txt = strings.Join(strings.Fields(txt), " ")
	rs := []rune(txt)
	if len(rs) > 180 {
		return string(rs[:180]) + "..."
	}
	return txt
}

func HashItem(it model.Item) string {
	u := strings.TrimSpace(strings.ToLower(it.URL))
	if parsed, err := url.Parse(u); err == nil && parsed.Host != "" {
		parsed.Fragment = ""
		u = parsed.String()
	}
	sum := sha256.Sum256([]byte(u + "\n" + strings.ToLower(strings.TrimSpace(it.Title))))
	return hex.EncodeToString(sum[:])
}

func Keywords(s string) []string {
	words := wordRE.FindAllString(strings.ToLower(s), -1)
	sort.Strings(words)
	return compact(words)
}

func boolScore(text string, kws []string) float64 {
	hits := 0
	for _, kw := range kws {
		if strings.Contains(text, kw) {
			hits++
		}
	}
	if hits == 0 {
		return 0
	}
	return math.Min(1, float64(hits)/2)
}

func compact(in []string) []string {
	out := in[:0]
	last := ""
	for _, s := range in {
		if s == "" || s == last {
			continue
		}
		out = append(out, s)
		last = s
	}
	return out
}

func Grade(score float64) string {
	switch {
	case score >= 0.70:
		return "S"
	case score >= 0.45:
		return "A"
	default:
		return "B"
	}
}

func Difficulty(score float64) string {
	if score > 0.65 {
		return "低到中"
	}
	return fmt.Sprintf("中等，规则分 %.2f", score)
}

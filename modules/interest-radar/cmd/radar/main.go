package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/kong-jing/meowbot/modules/interest-radar/internal/config"
	"github.com/kong-jing/meowbot/modules/interest-radar/internal/db"
	"github.com/kong-jing/meowbot/modules/interest-radar/internal/llm"
	"github.com/kong-jing/meowbot/modules/interest-radar/internal/meowapi"
	"github.com/kong-jing/meowbot/modules/interest-radar/internal/model"
	"github.com/kong-jing/meowbot/modules/interest-radar/internal/pipeline"
	"github.com/kong-jing/meowbot/modules/interest-radar/internal/planner"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	store, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		return err
	}
	if err := config.SeedTopics(context.Background(), store, cfg.TopicsFile); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd := "collect"
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		cmd = os.Args[1]
	}
	switch cmd {
	case "run-once", "collect":
		return pipeline.New(cfg, store).RunOnce(ctx)
	case "plan":
		fs := flag.NewFlagSet("plan", flag.ExitOnError)
		asJSON := fs.Bool("json", false, "print machine-readable plan json")
		_ = fs.Parse(os.Args[2:])
		tasks, source, err := buildPlan(ctx, cfg, store)
		if err != nil {
			return err
		}
		if *asJSON {
			return json.NewEncoder(os.Stdout).Encode(struct {
				Source string                  `json:"source"`
				Tasks  []model.ExplorationTask `json:"tasks"`
			}{Source: source, Tasks: tasks})
		}
		for i, t := range tasks {
			fmt.Printf("%d. %s\n   source: %s\n   reason: %s\n   tools: %s\n", i+1, t.Query, source, t.Reason, strings.Join(t.Tools, ", "))
		}
		return nil
	case "decay":
		return store.DecayTopics(ctx, time.Now())
	case "status":
		fmt.Println(store.DumpStatus(ctx))
		return nil
	case "backup":
		path, err := store.Backup(ctx, cfg.BackupDir)
		if err != nil {
			return err
		}
		fmt.Println(path)
		return nil
	case "digest":
		fs := flag.NewFlagSet("digest", flag.ExitOnError)
		send := fs.Bool("send", false, "send digest through meowbot")
		_ = fs.Parse(os.Args[2:])
		md, err := pipeline.New(cfg, store).BuildDigest(ctx, time.Now())
		if err != nil {
			return err
		}
		fmt.Println(md)
		if *send {
			return sendDigest(ctx, cfg, store, md)
		}
		return nil
	case "feedback":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: radar feedback <itemID> <useful|boring|save|track|block>")
		}
		itemID, err := strconv.ParseInt(os.Args[2], 10, 64)
		if err != nil {
			return fmt.Errorf("bad item id: %w", err)
		}
		msg, err := applyFeedback(ctx, store, itemID, os.Args[3])
		if err != nil {
			return err
		}
		fmt.Println(msg)
		return nil
	case "why":
		if len(os.Args) < 3 {
			return fmt.Errorf("usage: radar why <itemID>")
		}
		itemID, err := strconv.ParseInt(os.Args[2], 10, 64)
		if err != nil {
			return fmt.Errorf("bad item id: %w", err)
		}
		it, err := store.ItemByID(ctx, itemID)
		if err != nil {
			return err
		}
		fmt.Println(whyText(it))
		return nil
	case "items":
		limit := 12
		if len(os.Args) >= 3 {
			if v, err := strconv.Atoi(os.Args[2]); err == nil && v > 0 {
				limit = v
			}
		}
		items, err := store.TopItems(ctx, time.Now().AddDate(0, 0, -14), limit)
		if err != nil {
			return err
		}
		for i, it := range items {
			fmt.Printf("%d. id=%d score=%.2f %s\n", i+1, it.ID, it.Score, it.Title)
		}
		return nil
	case "track", "block", "more", "less", "focus":
		msg, err := applyTopicCommand(ctx, store, cmd, os.Args[2:])
		if err != nil {
			return err
		}
		fmt.Println(msg)
		return nil
	case "migrate":
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `interest-radar

Commands:
  collect           fetch RSS/arXiv/GitHub and score items
  plan [--json]     generate today's source plan; uses LLM JSON planner when configured
  decay             decay learned and temporary topic weights
  digest [--send]   build today's digest, optionally send it through meowbot
  feedback ID ACTION record Telegram feedback and update tag weights
  why ID            explain why an item was recommended
  items [N]         list recent top items with stable ids
  track TOPIC       pin a long-term interest
  block TOPIC       block or strongly down-rank a topic
  more|less TOPIC   gently adjust interest weight
  focus TOPIC DAYS  track a topic temporarily
  status            print SQLite item/topic/feedback counts
  backup            write a compact SQLite backup
  run-once          alias for collect
  migrate           create/update SQLite schema
`)
}

func buildPlan(ctx context.Context, cfg config.Config, store *db.Store) ([]model.ExplorationTask, string, error) {
	topics, err := store.Topics(ctx)
	if err != nil {
		return nil, "", err
	}
	now := time.Now()
	client := llm.New(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, cfg.OpenAIModel)
	if client.Enabled() {
		tasks, err := client.Plan(ctx, topics, now)
		if err == nil && len(tasks) > 0 {
			return tasks, "llm_json", nil
		}
	}
	return planner.Generate(topics, now), "rules", nil
}

func sendDigest(ctx context.Context, cfg config.Config, store *db.Store, md string) error {
	client := meowapi.New(cfg.MeowbotAPIURL, cfg.MeowbotAPIToken)
	items, err := store.TopItems(ctx, time.Now().AddDate(0, 0, -2), cfg.MaxDigestItems)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		if err := client.SendMessage(ctx, md); err != nil {
			return err
		}
		return store.SaveDigest(ctx, time.Now().Format("2006-01-02"), md, true)
	}
	for _, it := range items {
		card := digestCardText(it)
		if err := client.SendMessageWithMedia(ctx, card, it.ImageURL, radarActions(it.ID)); err != nil {
			return err
		}
	}
	return store.SaveDigest(ctx, time.Now().Format("2006-01-02"), md, true)
}

func digestCardText(it model.Item) string {
	title := strings.TrimSpace(it.Title)
	summary := strings.TrimSpace(it.Summary)
	if summary == "" {
		summary = "这条内容值得过一眼，适合继续跟进。"
	}
	reason := strings.TrimSpace(it.Reason)
	if reason == "" {
		reason = "和你当前兴趣方向匹配，可以先点开快速判断。"
	}

	var out strings.Builder
	fmt.Fprintf(&out, "#%d", it.ID)
	if src := strings.TrimSpace(it.Source); src != "" {
		fmt.Fprintf(&out, " | %s", src)
	}
	if title != "" {
		fmt.Fprintf(&out, "\n%s", title)
	}
	fmt.Fprintf(&out, "\n\n简介：%s", summary)
	fmt.Fprintf(&out, "\nAI评价：%s", reason)
	return out.String()
}

func radarActions(itemID int64) [][]meowapi.Button {
	id := strconv.FormatInt(itemID, 10)
	return [][]meowapi.Button{
		{
			{Text: "有用", CallbackData: "radar:useful:" + id},
			{Text: "无聊", CallbackData: "radar:boring:" + id},
			{Text: "收藏", CallbackData: "radar:save:" + id},
		},
	}
}

func applyFeedback(ctx context.Context, store *db.Store, itemID int64, action string) (string, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	delta, source, label, ok := feedbackEffect(action)
	if !ok {
		return "", fmt.Errorf("unknown feedback action %q", action)
	}
	it, err := store.ItemByID(ctx, itemID)
	if err != nil {
		return "", err
	}
	if err := store.AddFeedback(ctx, it.ID, action, "telegram"); err != nil {
		return "", err
	}
	changed := 0
	for _, tag := range it.Tags {
		name := db.NormalizeTopic(tag)
		if name == "" {
			continue
		}
		if err := store.AddTopicDelta(ctx, name, delta, source, []string{name}); err != nil {
			return "", err
		}
		changed++
	}
	if changed == 0 {
		return fmt.Sprintf("已记录：%s。这个条目没有可学习标签，只保存反馈。", label), nil
	}
	return fmt.Sprintf("已记录：%s。已调整 %d 个兴趣标签。", label, changed), nil
}

func feedbackEffect(action string) (delta float64, source, label string, ok bool) {
	switch action {
	case "useful":
		return 0.05, "learned", "有用", true
	case "boring":
		return -0.05, "learned", "无聊", true
	case "save":
		return 0.10, "learned", "收藏", true
	case "track":
		return 0.50, "pinned", "追踪类似", true
	case "block":
		return -1.00, "blocked", "屏蔽类似", true
	default:
		return 0, "", "", false
	}
}

func applyTopicCommand(ctx context.Context, store *db.Store, cmd string, args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: radar %s <topic>", cmd)
	}
	halfLife := 21.0
	expiresAt := (*time.Time)(nil)
	topicArgs := args
	if cmd == "focus" {
		if len(args) < 2 {
			return "", fmt.Errorf("usage: radar focus <topic> <days>")
		}
		days, err := strconv.Atoi(args[len(args)-1])
		if err != nil || days <= 0 {
			return "", fmt.Errorf("focus days must be a positive integer")
		}
		t := time.Now().AddDate(0, 0, days)
		expiresAt = &t
		halfLife = float64(days)
		topicArgs = args[:len(args)-1]
	}
	topic := db.NormalizeTopic(strings.Join(topicArgs, " "))
	if topic == "" {
		return "", fmt.Errorf("topic is empty")
	}
	switch cmd {
	case "track":
		_, err := store.UpsertTopic(ctx, model.Topic{Name: topic, Weight: 1.0, Source: "pinned", HalfLifeDays: 3650, Keywords: []string{topic}, UpdatedAt: time.Now()})
		return fmt.Sprintf("已长期追踪：%s", topic), err
	case "block":
		_, err := store.UpsertTopic(ctx, model.Topic{Name: topic, Weight: -1.0, Source: "blocked", HalfLifeDays: 3650, Keywords: []string{topic}, UpdatedAt: time.Now()})
		return fmt.Sprintf("已屏蔽方向：%s", topic), err
	case "more":
		return fmt.Sprintf("已增加兴趣权重：%s", topic), store.AddTopicDelta(ctx, topic, 0.10, "learned", []string{topic})
	case "less":
		return fmt.Sprintf("已降低兴趣权重：%s", topic), store.AddTopicDelta(ctx, topic, -0.10, "learned", []string{topic})
	case "focus":
		_, err := store.UpsertTopic(ctx, model.Topic{Name: topic, Weight: 0.8, Source: "temporary", HalfLifeDays: halfLife, ExpiresAt: expiresAt, Keywords: []string{topic}, UpdatedAt: time.Now()})
		return fmt.Sprintf("已临时聚焦：%s，到期：%s", topic, expiresAt.Format("2006-01-02")), err
	default:
		return "", fmt.Errorf("unknown topic command %q", cmd)
	}
}

func whyText(it model.Item) string {
	var b strings.Builder
	fmt.Fprintf(&b, "推荐原因：\n")
	if len(it.Tags) > 0 {
		fmt.Fprintf(&b, "- 命中兴趣标签：%s\n", strings.Join(it.Tags, " / "))
	}
	if strings.TrimSpace(it.Reason) != "" {
		fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(it.Reason))
	}
	if it.Score > 0 {
		fmt.Fprintf(&b, "- 综合分数：%.2f\n", it.Score)
	}
	if strings.TrimSpace(it.ImageURL) != "" {
		fmt.Fprintf(&b, "- 配图：%s\n", it.ImageURL)
	}
	if !it.PublishedAt.IsZero() {
		fmt.Fprintf(&b, "- 发布时间：%s\n", it.PublishedAt.Format("2006-01-02"))
	}
	fmt.Fprintf(&b, "- 来源：%s\n", it.Source)
	fmt.Fprintf(&b, "- 链接：%s\n", it.URL)
	return b.String()
}

package digest

import (
	"fmt"
	"strings"
	"time"

	"github.com/kong-jing/meowbot/modules/interest-radar/internal/model"
	"github.com/kong-jing/meowbot/modules/interest-radar/internal/scoring"
)

func Build(date time.Time, items []model.Item, tasks []model.ExplorationTask, max int) string {
	if max <= 0 {
		max = 8
	}
	if max > 8 {
		max = 8
	}
	if len(items) > max {
		items = items[:max]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# 今日私人雷达 %s\n\n", date.Format("2006-01-02"))
	section(&b, "今天最值得看", items)
	b.WriteString("## 今天主动探索了什么\n\n")
	if len(tasks) == 0 {
		b.WriteString("- 今天还没有生成探索任务。\n\n")
	} else {
		shown := len(tasks)
		if shown > 1 {
			shown = 1
		}
		for i, t := range tasks[:shown] {
			fmt.Fprintf(&b, "%d. %s\n   - 原因：%s\n   - 工具：%s\n   - 结果：发现 %d 条，%d 条进入日报\n", i+1, t.Query, t.Reason, strings.Join(t.Tools, ", "), t.Found, t.Selected)
		}
		if len(tasks) > shown {
			fmt.Fprintf(&b, "- 其余探索任务：%d 个已静默记录。\n", len(tasks)-shown)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func section(b *strings.Builder, title string, items []model.Item) {
	b.WriteString("## " + title + "\n\n")
	if len(items) == 0 {
		b.WriteString("- 暂无。\n\n")
		return
	}
	for i, it := range items {
		writeItem(b, i+1, it)
	}
}

func writeItem(b *strings.Builder, n int, it model.Item) {
	fmt.Fprintf(b, "%d. [%s] %s\n", n, scoring.Grade(it.Score), it.Title)
	fmt.Fprintf(b, "   - 摘要：%s\n", value(it.Summary, "暂无摘要"))
	fmt.Fprintf(b, "   - 为什么适合你：%s\n", value(it.Reason, "规则评分命中"))
	fmt.Fprintf(b, "   - 可玩性/难度：%s / %s\n", playability(it), scoring.Difficulty(it.Score))
	fmt.Fprintf(b, "   - 链接：%s\n", it.URL)
	if it.ID > 0 {
		fmt.Fprintf(b, "   - 评价编号：#%d\n", it.ID)
	}
	b.WriteString("\n")
}

func playability(it model.Item) string {
	text := strings.ToLower(it.Title + " " + it.Content + " " + it.URL)
	if strings.Contains(text, "github") || strings.Contains(text, "demo") || strings.Contains(text, "code") {
		return "高"
	}
	return "中"
}

func value(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

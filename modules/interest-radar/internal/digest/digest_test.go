package digest

import (
	"strings"
	"testing"
	"time"

	"github.com/kong-jing/meowbot/modules/interest-radar/internal/model"
)

func TestBuildIncludesExploration(t *testing.T) {
	md := Build(time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC), []model.Item{{Title: "ESP32 demo", URL: "https://example.com", Score: 0.8, Summary: "x", Reason: "y"}}, []model.ExplorationTask{{Query: "ESP32", Reason: "test", Tools: []string{"github"}, Found: 2, Selected: 1}}, 8)
	if !strings.Contains(md, "今天主动探索了什么") || !strings.Contains(md, "ESP32") {
		t.Fatalf("missing content:\n%s", md)
	}
}

func TestBuildCapsDigestItemsForHumanScan(t *testing.T) {
	var items []model.Item
	for i := 0; i < 12; i++ {
		items = append(items, model.Item{ID: int64(i + 1), Title: "item", URL: "https://example.com", Score: 0.8})
	}
	var tasks []model.ExplorationTask
	for i := 0; i < 4; i++ {
		tasks = append(tasks, model.ExplorationTask{Query: "query", Reason: "reason", Tools: []string{"github"}})
	}
	md := Build(time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC), items, tasks, 12)
	if got := strings.Count(md, "评价编号：#"); got != 8 {
		t.Fatalf("digest items=%d, want 8\n%s", got, md)
	}
	if got := numberedLines(md); got > 9 {
		t.Fatalf("numbered lines=%d, want <=9\n%s", got, md)
	}
}

func numberedLines(s string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if len(line) >= 3 && line[0] >= '0' && line[0] <= '9' && line[1] == '.' && line[2] == ' ' {
			n++
		}
	}
	return n
}

package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kong-jing/meowbot/modules/interest-radar/internal/db"
	"github.com/kong-jing/meowbot/modules/interest-radar/internal/model"
)

func TestApplyFeedbackUpdatesTaggedTopics(t *testing.T) {
	ctx := context.Background()
	store, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}

	id, inserted, err := store.UpsertItem(ctx, model.Item{
		Source:    "test",
		Title:     "ESP32 TinyML camera",
		URL:       "https://example.com/esp32-tinyml",
		FetchedAt: time.Now(),
		Hash:      "test-hash",
		Tags:      []string{"esp32", "tinyml"},
		Score:     0.8,
		Status:    "new",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !inserted {
		t.Fatal("expected inserted item")
	}

	msg, err := applyFeedback(ctx, store, id, "useful")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "已调整 2 个兴趣标签") {
		t.Fatalf("unexpected message: %s", msg)
	}
	status := store.DumpStatus(ctx)
	if !strings.Contains(status, "feedback=1") {
		t.Fatalf("feedback was not recorded: %s", status)
	}
	topics, err := store.Topics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, topic := range topics {
		got[topic.Name] = topic.Weight
	}
	if got["esp32"] <= 0 || got["tinyml"] <= 0 {
		t.Fatalf("topics were not positively updated: %#v", got)
	}
}

func TestDigestCardTextIncludesUnifiedCardContent(t *testing.T) {
	card := digestCardText(model.Item{
		ID:       273,
		Source:   "github",
		Title:    "ESP32-S3 camera tinyml demo",
		Summary:  "把摄像头推理和板载部署揉到了一起。",
		Reason:   "题材贴近你现在的边缘 AI 和可落地 demo 兴趣。",
		ImageURL: "https://example.com/cover.jpg",
	})

	for _, want := range []string{
		"#273 | github",
		"ESP32-S3 camera tinyml demo",
		"简介：把摄像头推理和板载部署揉到了一起。",
		"AI评价：题材贴近你现在的边缘 AI 和可落地 demo 兴趣。",
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("card missing %q:\n%s", want, card)
		}
	}
}

func TestDigestCardTextFallsBackWhenSummaryMissing(t *testing.T) {
	card := digestCardText(model.Item{
		ID:     9,
		Source: "rss",
		Title:  "Interesting build log",
	})

	for _, want := range []string{
		"简介：这条内容值得过一眼，适合继续跟进。",
		"AI评价：和你当前兴趣方向匹配，可以先点开快速判断。",
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("card missing fallback %q:\n%s", want, card)
		}
	}
}

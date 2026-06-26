package db

import (
	"context"
	"testing"
	"time"

	"github.com/kong-jing/meowbot/modules/interest-radar/internal/model"
)

func TestDecayTopicsSkipsPinnedAndDecaysLearned(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	_, _ = store.UpsertTopic(context.Background(), model.Topic{Name: "pinned", Weight: 1, Source: "pinned", HalfLifeDays: 7, UpdatedAt: now.AddDate(0, 0, -7)})
	_, _ = store.UpsertTopic(context.Background(), model.Topic{Name: "learned", Weight: 1, Source: "learned", HalfLifeDays: 7, UpdatedAt: now.AddDate(0, 0, -7)})
	if err := store.DecayTopics(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	topics, _ := store.Topics(context.Background())
	got := map[string]float64{}
	for _, topic := range topics {
		got[topic.Name] = topic.Weight
	}
	if got["pinned"] != 1 {
		t.Fatalf("pinned changed: %v", got["pinned"])
	}
	if got["learned"] >= 1 || got["learned"] <= 0 {
		t.Fatalf("learned did not decay sensibly: %v", got["learned"])
	}
}

func TestItemImageURLPersists(t *testing.T) {
	ctx := context.Background()
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}
	id, _, err := store.UpsertItem(ctx, model.Item{
		Source:    "test",
		Title:     "with image",
		URL:       "https://example.com/item",
		ImageURL:  "https://example.com/image.jpg",
		FetchedAt: time.Now(),
		Hash:      "image-hash",
		Status:    "new",
	})
	if err != nil {
		t.Fatal(err)
	}
	it, err := store.ItemByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if it.ImageURL != "https://example.com/image.jpg" {
		t.Fatalf("image url=%q", it.ImageURL)
	}
}

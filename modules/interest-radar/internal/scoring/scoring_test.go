package scoring

import (
	"testing"

	"github.com/kong-jing/meowbot/modules/interest-radar/internal/model"
)

func TestScoreExplainableAndBlocksCrypto(t *testing.T) {
	topics := []model.Topic{{Name: "esp32", Weight: 1, Source: "pinned", Keywords: []string{"esp32"}}, {Name: "crypto", Weight: -1, Source: "blocked", Keywords: []string{"crypto"}}}
	good := Score(model.Item{Title: "ESP32-S3 TinyML camera open source GitHub demo"}, topics)
	if good.Score <= 0 || len(good.Tags) == 0 || good.Status == "ignored" {
		t.Fatalf("unexpected good score: %+v", good)
	}
	bad := Score(model.Item{Title: "Crypto airdrop AI agent"}, topics)
	if bad.Status != "ignored" {
		t.Fatalf("crypto should be ignored: %+v", bad)
	}
}

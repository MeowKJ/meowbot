package llm

import (
	"testing"
	"time"
)

func TestParsePlanJSONValidatesToolsAndCapsTasks(t *testing.T) {
	now := time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC)
	raw := `{"tasks":[
		{"query":"ESP32-S3 TinyML camera open source","reason":"edge AI hardware is active","tools":["github_search","arxiv","browser"],"ttl_days":30},
		{"query":"   ","reason":"bad","tools":["github_search"],"ttl_days":7}
	]}`
	tasks, err := parsePlanJSON(raw, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks=%d", len(tasks))
	}
	if tasks[0].Query != "ESP32-S3 TinyML camera open source" {
		t.Fatalf("query=%q", tasks[0].Query)
	}
	if len(tasks[0].Tools) != 2 || tasks[0].Tools[0] != "github_search" || tasks[0].Tools[1] != "arxiv" {
		t.Fatalf("tools=%v", tasks[0].Tools)
	}
	if tasks[0].ExpiresAt.Sub(now) != 14*24*time.Hour {
		t.Fatalf("ttl=%v", tasks[0].ExpiresAt.Sub(now))
	}
}

func TestCleanQueryRemovesWebSearchSyntax(t *testing.T) {
	got := cleanQuery(`(ESP32-S3 OR TinyML) site:github.com "camera demo" https://example.com`)
	want := "ESP32-S3 TinyML camera demo"
	if got != want {
		t.Fatalf("query=%q want %q", got, want)
	}
}

func TestCleanTagsNormalizesAndCaps(t *testing.T) {
	got := cleanTags([]string{"ESP32 S3", "ESP32 S3", " tinyml ", "this-tag-is-way-too-long-for-a-human-interest-model-because-it-never-ends", "robotics"})
	want := []string{"esp32_s3", "tinyml", "robotics"}
	if len(got) != len(want) {
		t.Fatalf("tags=%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tags=%v", got)
		}
	}
}

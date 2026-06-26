package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kong-jing/meowbot/modules/interest-radar/internal/db"
	"github.com/kong-jing/meowbot/modules/interest-radar/internal/model"
	"gopkg.in/yaml.v3"
)

type Config struct {
	DBPath          string
	BackupDir       string
	SourcesFile     string
	TopicsFile      string
	DigestTime      string
	MeowbotAPIURL   string
	MeowbotAPIToken string
	GitHubToken     string
	OpenAIAPIKey    string
	OpenAIBaseURL   string
	OpenAIModel     string
	MaxDigestItems  int
}

func Load() (Config, error) {
	cfg := Config{
		DBPath:          env("RADAR_DB_PATH", "data/radar.db"),
		BackupDir:       env("RADAR_BACKUP_DIR", "data/backups"),
		SourcesFile:     env("RADAR_SOURCES_FILE", "config/radar-sources.yaml"),
		TopicsFile:      env("RADAR_TOPICS_FILE", "config/radar-topics.yaml"),
		DigestTime:      env("RADAR_DIGEST_TIME", "08:30"),
		MeowbotAPIURL:   env("MEOWBOT_API_URL", "http://127.0.0.1:8765"),
		MeowbotAPIToken: os.Getenv("MEOWBOT_API_TOKEN"),
		GitHubToken:     os.Getenv("GITHUB_TOKEN"),
		OpenAIAPIKey:    os.Getenv("OPENAI_API_KEY"),
		OpenAIBaseURL:   env("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		OpenAIModel:     os.Getenv("OPENAI_MODEL"),
		MaxDigestItems:  clampInt(env("RADAR_MAX_DIGEST_ITEMS", "8"), 8, 1, 8),
	}
	return cfg, nil
}

func LoadSources(path string) (model.SourceConfig, error) {
	var out model.SourceConfig
	b, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	return out, yaml.Unmarshal(b, &out)
}

type topicSeed struct {
	Name         string   `yaml:"name"`
	Weight       float64  `yaml:"weight"`
	Source       string   `yaml:"source"`
	HalfLifeDays float64  `yaml:"half_life_days"`
	Keywords     []string `yaml:"keywords"`
}

func SeedTopics(ctx context.Context, store *db.Store, path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var seeds []topicSeed
	if err := yaml.Unmarshal(b, &seeds); err != nil {
		return err
	}
	for _, s := range seeds {
		if s.HalfLifeDays == 0 {
			s.HalfLifeDays = 21
		}
		t := model.Topic{
			Name:         s.Name,
			Weight:       s.Weight,
			Source:       s.Source,
			HalfLifeDays: s.HalfLifeDays,
			Keywords:     s.Keywords,
			UpdatedAt:    time.Now(),
		}
		if len(t.Keywords) == 0 {
			t.Keywords = []string{s.Name}
		}
		if t.Source == "" {
			t.Source = "pinned"
		}
		if _, err := store.UpsertTopic(ctx, t); err != nil {
			return fmt.Errorf("seed topic %q: %w", s.Name, err)
		}
	}
	return nil
}

func EncodeStrings(v []string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func DecodeStrings(s string) []string {
	var out []string
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func mustInt(s string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return v
}

func clampInt(s string, def, min, max int) int {
	v := mustInt(s, def)
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func mustInt64(s string, def int64) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return def
	}
	return v
}

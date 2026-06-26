package pipeline

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/kong-jing/meowbot/modules/interest-radar/internal/config"
	"github.com/kong-jing/meowbot/modules/interest-radar/internal/db"
	"github.com/kong-jing/meowbot/modules/interest-radar/internal/digest"
	"github.com/kong-jing/meowbot/modules/interest-radar/internal/fetcher"
	"github.com/kong-jing/meowbot/modules/interest-radar/internal/llm"
	"github.com/kong-jing/meowbot/modules/interest-radar/internal/model"
	"github.com/kong-jing/meowbot/modules/interest-radar/internal/planner"
	"github.com/kong-jing/meowbot/modules/interest-radar/internal/scoring"
)

type Pipeline struct {
	cfg   config.Config
	store *db.Store
	llm   *llm.Client
}

func New(cfg config.Config, store *db.Store) *Pipeline {
	return &Pipeline{cfg: cfg, store: store, llm: llm.New(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, cfg.OpenAIModel)}
}

func (p *Pipeline) RunOnce(ctx context.Context) error {
	if err := p.store.DecayTopics(ctx, time.Now()); err != nil {
		return err
	}
	sources, err := config.LoadSources(p.cfg.SourcesFile)
	if err != nil {
		return err
	}
	topics, err := p.store.Topics(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	tasks := planner.Generate(topics, now)
	if p.llm.Enabled() {
		if aiTasks, err := p.llm.Plan(ctx, topics, now); err == nil && len(aiTasks) > 0 {
			tasks = aiTasks
		} else if err != nil {
			log.Printf("llm planner failed, fallback to rules: %v", err)
		}
	}
	for _, t := range tasks {
		_ = p.store.AddTask(ctx, t)
	}
	var all []model.Item
	rss := fetcher.RSSFetcher{}
	for _, src := range sources.RSS {
		items, err := rss.Fetch(ctx, src.Name, src.URL)
		if err != nil {
			log.Printf("rss %s failed: %v", src.Name, err)
			continue
		}
		all = append(all, items...)
	}
	gh := fetcher.GitHubFetcher{Token: p.cfg.GitHubToken}
	for _, src := range sources.GitHub {
		items, err := gh.Search(ctx, src.Query, 10)
		if err != nil {
			log.Printf("github %s failed: %v", src.Query, err)
			continue
		}
		all = append(all, items...)
	}
	for _, repo := range sources.Releases {
		items, err := gh.Releases(ctx, repo, 5)
		if err != nil {
			log.Printf("github releases %s failed: %v", repo, err)
			continue
		}
		all = append(all, items...)
	}
	ax := fetcher.ArxivFetcher{}
	items, err := ax.Fetch(ctx, sources.Arxiv.Categories, "", 25)
	if err != nil {
		log.Printf("arxiv failed: %v", err)
	} else {
		all = append(all, items...)
	}
	for _, task := range tasks {
		found := 0
		selected := 0
		for _, tool := range task.Tools {
			var items []model.Item
			var err error
			switch tool {
			case "github_search":
				items, err = gh.Search(ctx, task.Query, 8)
			case "arxiv":
				items, err = ax.Fetch(ctx, sources.Arxiv.Categories, task.Query, 8)
			default:
				continue
			}
			if err != nil {
				log.Printf("task %s via %s failed: %v", task.Query, tool, err)
				continue
			}
			found += len(items)
			for _, it := range items {
				if p.ingest(ctx, it, topics) {
					selected++
				}
			}
		}
		task.Found = found
		task.Selected = selected
		task.Status = "done"
		_ = p.store.AddTask(ctx, task)
	}
	for _, it := range all {
		p.ingest(ctx, it, topics)
	}
	return nil
}

func (p *Pipeline) BuildDigest(ctx context.Context, now time.Time) (string, error) {
	items, err := p.store.TopItems(ctx, now.AddDate(0, 0, -2), p.cfg.MaxDigestItems)
	if err != nil {
		return "", err
	}
	tasks, err := p.store.RecentTasks(ctx, now.AddDate(0, 0, -1))
	if err != nil {
		return "", err
	}
	md := digest.Build(now, items, tasks, p.cfg.MaxDigestItems)
	return md, p.store.SaveDigest(ctx, now.Format("2006-01-02"), md, false)
}

func (p *Pipeline) ingest(ctx context.Context, it model.Item, topics []model.Topic) bool {
	if strings.TrimSpace(it.Title) == "" || strings.TrimSpace(it.URL) == "" {
		return false
	}
	it.Hash = scoring.HashItem(it)
	it.Summary = scoring.Summary(it)
	res := scoring.Score(it, topics)
	it.Tags, it.Score, it.Reason, it.Status = res.Tags, res.Score, res.Reason, res.Status
	if it.Score > 0.55 && p.llm.Enabled() {
		if sum, reason, tags, err := p.llm.Enrich(ctx, it, res.Reason, res.Tags); err == nil {
			if sum != "" {
				it.Summary = sum
			}
			if reason != "" {
				it.Reason = reason
			}
			it.Tags = mergeTags(it.Tags, tags)
		}
	}
	id, inserted, err := p.store.UpsertItem(ctx, it)
	if err != nil {
		log.Printf("upsert item failed: %v", err)
		return false
	}
	_ = p.store.UpdateItemScore(ctx, id, it.Tags, it.Score, it.Summary, it.Reason, it.Status, it.ImageURL)
	return inserted && it.Status != "ignored"
}

func mergeTags(base, extra []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, tag := range append(base, extra...) {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		out = append(out, tag)
		seen[tag] = true
	}
	return out
}

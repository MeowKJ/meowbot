package fetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kong-jing/meowbot/modules/interest-radar/internal/model"
)

type GitHubFetcher struct {
	Client *http.Client
	Token  string
}

func (f GitHubFetcher) Search(ctx context.Context, query string, max int) ([]model.Item, error) {
	if max <= 0 {
		max = 10
	}
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	q := query
	if !strings.Contains(q, "pushed:") {
		q += " pushed:>=" + time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	}
	u := "https://api.github.com/search/repositories?q=" + url.QueryEscape(q) + fmt.Sprintf("&sort=updated&order=desc&per_page=%d", max)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "interest-radar/0.1")
	if f.Token != "" {
		req.Header.Set("Authorization", "Bearer "+f.Token)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("github search %s: %s", res.Status, strings.TrimSpace(string(b)))
	}
	var body ghSearch
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, err
	}
	now := time.Now()
	var out []model.Item
	for _, r := range body.Items {
		out = append(out, model.Item{
			Source:      "github",
			Title:       r.FullName,
			URL:         r.HTMLURL,
			ImageURL:    githubOpenGraphImage(r.FullName),
			Author:      strings.Split(r.FullName, "/")[0],
			Content:     strings.TrimSpace(r.Description + " " + strings.Join(r.Topics, " ") + fmt.Sprintf(" stars:%d language:%s", r.Stars, r.Language)),
			PublishedAt: r.PushedAt,
			FetchedAt:   now,
			Status:      "new",
		})
	}
	return out, nil
}

func (f GitHubFetcher) Releases(ctx context.Context, repo string, max int) ([]model.Item, error) {
	if max <= 0 {
		max = 5
	}
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	u := "https://api.github.com/repos/" + strings.TrimSpace(repo) + "/releases?per_page=" + fmt.Sprint(max)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "interest-radar/0.1")
	if f.Token != "" {
		req.Header.Set("Authorization", "Bearer "+f.Token)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("github releases %s: %s", res.Status, strings.TrimSpace(string(b)))
	}
	var releases []struct {
		Name        string    `json:"name"`
		TagName     string    `json:"tag_name"`
		HTMLURL     string    `json:"html_url"`
		Body        string    `json:"body"`
		PublishedAt time.Time `json:"published_at"`
	}
	if err := json.NewDecoder(res.Body).Decode(&releases); err != nil {
		return nil, err
	}
	now := time.Now()
	var out []model.Item
	for _, r := range releases {
		title := repo + " " + firstNonEmpty(r.Name, r.TagName)
		out = append(out, model.Item{
			Source:      "github_release",
			Title:       title,
			URL:         r.HTMLURL,
			ImageURL:    githubOpenGraphImage(repo),
			Author:      strings.Split(repo, "/")[0],
			Content:     r.Body,
			PublishedAt: r.PublishedAt,
			FetchedAt:   now,
			Status:      "new",
		})
	}
	return out, nil
}

func githubOpenGraphImage(repo string) string {
	repo = strings.Trim(strings.TrimSpace(repo), "/")
	if repo == "" || !strings.Contains(repo, "/") {
		return ""
	}
	return "https://opengraph.githubassets.com/interest-radar/" + repo
}

type ghSearch struct {
	Items []struct {
		FullName    string    `json:"full_name"`
		HTMLURL     string    `json:"html_url"`
		Description string    `json:"description"`
		Language    string    `json:"language"`
		Stars       int       `json:"stargazers_count"`
		PushedAt    time.Time `json:"pushed_at"`
		Topics      []string  `json:"topics"`
	} `json:"items"`
}

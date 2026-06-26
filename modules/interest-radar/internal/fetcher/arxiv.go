package fetcher

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kong-jing/meowbot/modules/interest-radar/internal/model"
)

type ArxivFetcher struct{ Client *http.Client }

func (f ArxivFetcher) Fetch(ctx context.Context, categories []string, query string, max int) ([]model.Item, error) {
	if max <= 0 {
		max = 20
	}
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	terms := []string{}
	for _, cat := range categories {
		terms = append(terms, "cat:"+cat)
	}
	if strings.TrimSpace(query) != "" {
		terms = append(terms, "all:"+strings.ReplaceAll(query, " ", "+"))
	}
	u := "https://export.arxiv.org/api/query?search_query=" + url.QueryEscape(strings.Join(terms, " OR ")) + fmt.Sprintf("&sortBy=submittedDate&sortOrder=descending&max_results=%d", max)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("User-Agent", "interest-radar/0.1")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var feed arxivFeed
	if err := xml.NewDecoder(res.Body).Decode(&feed); err != nil {
		return nil, err
	}
	now := time.Now()
	var out []model.Item
	for _, e := range feed.Entries {
		link := e.ID
		for _, l := range e.Links {
			if l.Rel == "alternate" && l.Href != "" {
				link = l.Href
			}
		}
		authors := []string{}
		for _, a := range e.Authors {
			authors = append(authors, a.Name)
		}
		out = append(out, model.Item{
			Source:      "arxiv",
			Title:       clean(e.Title),
			URL:         strings.TrimSpace(link),
			Author:      strings.Join(authors, ", "),
			Content:     clean(e.Summary),
			PublishedAt: parseAnyTime(e.Published, e.Updated),
			FetchedAt:   now,
			Status:      "new",
		})
	}
	return out, nil
}

type arxivFeed struct {
	Entries []arxivEntry `xml:"entry"`
}

type arxivEntry struct {
	ID        string `xml:"id"`
	Title     string `xml:"title"`
	Summary   string `xml:"summary"`
	Published string `xml:"published"`
	Updated   string `xml:"updated"`
	Authors   []struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Links []struct {
		Href string `xml:"href,attr"`
		Rel  string `xml:"rel,attr"`
	} `xml:"link"`
}

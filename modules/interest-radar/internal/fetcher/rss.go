package fetcher

import (
	"context"
	"encoding/xml"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/kong-jing/meowbot/modules/interest-radar/internal/model"
)

type RSSFetcher struct{ Client *http.Client }

var tagRE = regexp.MustCompile(`<[^>]+>`)
var imgRE = regexp.MustCompile(`(?i)<img[^>]+src=["']([^"']+)["']`)

func (f RSSFetcher) Fetch(ctx context.Context, name, feedURL string) ([]model.Item, error) {
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	req.Header.Set("User-Agent", "interest-radar/0.1")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var feed xmlFeed
	if err := xml.NewDecoder(res.Body).Decode(&feed); err != nil {
		return nil, err
	}
	var out []model.Item
	now := time.Now()
	for _, e := range feed.AllItems() {
		title := clean(e.Title)
		link := e.LinkHref()
		if title == "" || link == "" {
			continue
		}
		out = append(out, model.Item{
			Source:      "rss:" + name,
			Title:       title,
			URL:         link,
			ImageURL:    e.ImageURL(),
			Author:      clean(e.Author.Name),
			Content:     clean(firstNonEmpty(e.Description, e.Summary, e.Content)),
			PublishedAt: parseAnyTime(e.PubDate, e.Updated, e.Published),
			FetchedAt:   now,
			Status:      "new",
		})
	}
	return out, nil
}

type xmlFeed struct {
	Channel struct {
		Items []xmlEntry `xml:"item"`
	} `xml:"channel"`
	Entries []xmlEntry `xml:"entry"`
}

type xmlEntry struct {
	Title       string    `xml:"title"`
	Links       []xmlLink `xml:"link"`
	Description string    `xml:"description"`
	Summary     string    `xml:"summary"`
	Content     string    `xml:"content"`
	Enclosure   xmlMedia  `xml:"enclosure"`
	Thumbnail   xmlMedia  `xml:"thumbnail"`
	PubDate     string    `xml:"pubDate"`
	Updated     string    `xml:"updated"`
	Published   string    `xml:"published"`
	Author      struct {
		Name string `xml:"name"`
	} `xml:"author"`
}

type xmlLink struct {
	Text string `xml:",chardata"`
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

type xmlMedia struct {
	URL    string `xml:"url,attr"`
	Type   string `xml:"type,attr"`
	Medium string `xml:"medium,attr"`
}

func (e xmlEntry) LinkHref() string {
	for _, l := range e.Links {
		if strings.TrimSpace(l.Text) != "" {
			return strings.TrimSpace(l.Text)
		}
		if l.Href != "" && (l.Rel == "" || l.Rel == "alternate") {
			return strings.TrimSpace(l.Href)
		}
	}
	return ""
}

func (e xmlEntry) ImageURL() string {
	candidates := []string{
		e.Thumbnail.URL,
		e.Enclosure.URL,
		firstImageSrc(e.Content),
		firstImageSrc(e.Description),
		firstImageSrc(e.Summary),
	}
	for _, raw := range candidates {
		if u := cleanImageURL(raw); u != "" {
			return u
		}
	}
	return ""
}

func (f xmlFeed) AllItems() []xmlEntry {
	if len(f.Channel.Items) > 0 {
		return f.Channel.Items
	}
	return f.Entries
}

func clean(s string) string {
	s = tagRE.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Join(strings.Fields(s), " ")
}

func firstImageSrc(s string) string {
	m := imgRE.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return html.UnescapeString(m[1])
}

func cleanImageURL(raw string) string {
	raw = strings.TrimSpace(html.UnescapeString(raw))
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.String()
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func parseAnyTime(v ...string) time.Time {
	layouts := []string{time.RFC1123Z, time.RFC1123, time.RFC3339, "Mon, 02 Jan 2006 15:04:05 -0700", "2006-01-02T15:04:05Z"}
	for _, raw := range v {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		for _, layout := range layouts {
			if t, err := time.Parse(layout, raw); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

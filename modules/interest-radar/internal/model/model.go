package model

import "time"

type Item struct {
	ID          int64
	Source      string
	Title       string
	URL         string
	ImageURL    string
	Author      string
	Content     string
	Summary     string
	PublishedAt time.Time
	FetchedAt   time.Time
	Hash        string
	Tags        []string
	Score       float64
	Reason      string
	Status      string
}

type Topic struct {
	ID           int64
	Name         string
	Weight       float64
	Source       string
	HalfLifeDays float64
	ExpiresAt    *time.Time
	Keywords     []string
	UpdatedAt    time.Time
}

type Feedback struct {
	ID        int64
	ItemID    int64
	Action    string
	Note      string
	CreatedAt time.Time
}

type ExplorationTask struct {
	ID        int64
	Query     string
	Reason    string
	Tools     []string
	Status    string
	CreatedAt time.Time
	ExpiresAt time.Time
	Found     int
	Selected  int
}

type SourceConfig struct {
	RSS      []RSSSource    `yaml:"rss"`
	GitHub   []GitHubSource `yaml:"github"`
	Releases []string       `yaml:"releases"`
	Arxiv    ArxivSource    `yaml:"arxiv"`
}

type RSSSource struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

type GitHubSource struct {
	Query string `yaml:"query"`
}

type ArxivSource struct {
	Categories []string `yaml:"categories"`
}

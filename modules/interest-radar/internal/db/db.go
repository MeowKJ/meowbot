package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kong-jing/meowbot/modules/interest-radar/internal/model"
	_ "modernc.org/sqlite"
)

type Store struct {
	db   *sql.DB
	path string
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db, path: path}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Migrate() error {
	schema := `
CREATE TABLE IF NOT EXISTS items (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source TEXT NOT NULL,
  title TEXT NOT NULL,
  url TEXT NOT NULL,
  image_url TEXT,
  author TEXT,
  content TEXT,
  summary TEXT,
  published_at DATETIME,
  fetched_at DATETIME NOT NULL,
  hash TEXT NOT NULL UNIQUE,
  tags_json TEXT NOT NULL DEFAULT '[]',
  score REAL NOT NULL DEFAULT 0,
  reason TEXT,
  status TEXT NOT NULL DEFAULT 'new'
);
CREATE INDEX IF NOT EXISTS idx_items_score ON items(score DESC);
CREATE INDEX IF NOT EXISTS idx_items_published ON items(published_at DESC);

CREATE TABLE IF NOT EXISTS topics (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  weight REAL NOT NULL,
  source TEXT NOT NULL,
  half_life_days REAL NOT NULL,
  expires_at DATETIME,
  keywords_json TEXT NOT NULL DEFAULT '[]',
  updated_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_topics_weight ON topics(weight DESC);

CREATE TABLE IF NOT EXISTS feedback (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  item_id INTEGER,
  action TEXT NOT NULL,
  note TEXT,
  created_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS digests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  date TEXT NOT NULL UNIQUE,
  markdown TEXT NOT NULL,
  sent_at DATETIME
);

CREATE TABLE IF NOT EXISTS exploration_tasks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  query TEXT NOT NULL,
  reason TEXT,
  tools_json TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  expires_at DATETIME NOT NULL,
  found INTEGER NOT NULL DEFAULT 0,
  selected INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	return s.ensureColumn("items", "image_url", "TEXT")
}

func (s *Store) UpsertItem(ctx context.Context, it model.Item) (int64, bool, error) {
	tags := encode(it.Tags)
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO items
(source,title,url,image_url,author,content,summary,published_at,fetched_at,hash,tags_json,score,reason,status)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, it.Source, it.Title, it.URL, it.ImageURL, it.Author, it.Content, it.Summary, nullTime(it.PublishedAt), it.FetchedAt, it.Hash, tags, it.Score, it.Reason, it.Status)
	if err != nil {
		return 0, false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var id int64
		err := s.db.QueryRowContext(ctx, `SELECT id FROM items WHERE hash=?`, it.Hash).Scan(&id)
		return id, false, err
	}
	id, err := res.LastInsertId()
	return id, true, err
}

func (s *Store) UpdateItemScore(ctx context.Context, id int64, tags []string, score float64, summary, reason, status, imageURL string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE items SET tags_json=?, score=?, summary=?, reason=?, status=?, image_url=COALESCE(NULLIF(?, ''), image_url) WHERE id=?`, encode(tags), score, summary, reason, status, imageURL, id)
	return err
}

func (s *Store) TopItems(ctx context.Context, since time.Time, limit int) ([]model.Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,source,title,url,image_url,author,content,summary,published_at,fetched_at,hash,tags_json,score,reason,status
FROM items WHERE fetched_at >= ? AND status != 'ignored' ORDER BY score DESC, published_at DESC LIMIT ?`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows)
}

func (s *Store) ItemByNumber(ctx context.Context, n int) (model.Item, error) {
	items, err := s.TopItems(ctx, time.Now().AddDate(0, 0, -14), 50)
	if err != nil {
		return model.Item{}, err
	}
	if n <= 0 || n > len(items) {
		return model.Item{}, sql.ErrNoRows
	}
	return items[n-1], nil
}

func (s *Store) ItemByID(ctx context.Context, id int64) (model.Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,source,title,url,image_url,author,content,summary,published_at,fetched_at,hash,tags_json,score,reason,status
FROM items WHERE id=?`, id)
	if err != nil {
		return model.Item{}, err
	}
	defer rows.Close()
	items, err := scanItems(rows)
	if err != nil {
		return model.Item{}, err
	}
	if len(items) == 0 {
		return model.Item{}, sql.ErrNoRows
	}
	return items[0], nil
}

func (s *Store) UpsertTopic(ctx context.Context, t model.Topic) (int64, error) {
	if t.HalfLifeDays == 0 {
		t.HalfLifeDays = 21
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = time.Now()
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO topics(name,weight,source,half_life_days,expires_at,keywords_json,updated_at)
VALUES(?,?,?,?,?,?,?)
ON CONFLICT(name) DO UPDATE SET
weight=excluded.weight,
source=CASE WHEN topics.source='pinned' AND excluded.source!='blocked' THEN topics.source ELSE excluded.source END,
half_life_days=excluded.half_life_days,
expires_at=excluded.expires_at,
keywords_json=excluded.keywords_json,
updated_at=excluded.updated_at`, t.Name, t.Weight, t.Source, t.HalfLifeDays, t.ExpiresAt, encode(t.Keywords), t.UpdatedAt)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

func (s *Store) AddTopicDelta(ctx context.Context, name string, delta float64, source string, keywords []string) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO topics(name,weight,source,half_life_days,keywords_json,updated_at)
VALUES(?,?,?,?,?,?)
ON CONFLICT(name) DO UPDATE SET
weight=max(-1.0, min(1.5, topics.weight + excluded.weight)),
source=CASE WHEN excluded.source='blocked' THEN 'blocked' WHEN topics.source='pinned' THEN 'pinned' ELSE excluded.source END,
keywords_json=CASE WHEN topics.keywords_json='[]' THEN excluded.keywords_json ELSE topics.keywords_json END,
updated_at=excluded.updated_at`, name, delta, source, 21.0, encode(keywords), now)
	return err
}

func (s *Store) Topics(ctx context.Context) ([]model.Topic, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,weight,source,half_life_days,expires_at,keywords_json,updated_at FROM topics ORDER BY weight DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Topic
	for rows.Next() {
		var t model.Topic
		var exp sql.NullTime
		var kw string
		if err := rows.Scan(&t.ID, &t.Name, &t.Weight, &t.Source, &t.HalfLifeDays, &exp, &kw, &t.UpdatedAt); err != nil {
			return nil, err
		}
		if exp.Valid {
			t.ExpiresAt = &exp.Time
		}
		t.Keywords = decode(kw)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) DecayTopics(ctx context.Context, now time.Time) error {
	topics, err := s.Topics(ctx)
	if err != nil {
		return err
	}
	for _, t := range topics {
		if t.Source == "pinned" || t.Source == "blocked" {
			continue
		}
		if t.ExpiresAt != nil && now.After(*t.ExpiresAt) {
			_, _ = s.db.ExecContext(ctx, `DELETE FROM topics WHERE id=? AND source='temporary'`, t.ID)
			continue
		}
		days := now.Sub(t.UpdatedAt).Hours() / 24
		if days <= 0 || t.HalfLifeDays <= 0 {
			continue
		}
		newWeight := t.Weight * powHalf(days/t.HalfLifeDays)
		_, err := s.db.ExecContext(ctx, `UPDATE topics SET weight=?, updated_at=? WHERE id=?`, newWeight, now, t.ID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) AddFeedback(ctx context.Context, itemID int64, action, note string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO feedback(item_id,action,note,created_at) VALUES(?,?,?,?)`, itemID, action, note, time.Now())
	return err
}

func (s *Store) SaveDigest(ctx context.Context, date, markdown string, sent bool) error {
	var sentAt any
	if sent {
		sentAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO digests(date,markdown,sent_at) VALUES(?,?,?)
ON CONFLICT(date) DO UPDATE SET markdown=excluded.markdown, sent_at=COALESCE(excluded.sent_at,digests.sent_at)`, date, markdown, sentAt)
	return err
}

func (s *Store) AddTask(ctx context.Context, t model.ExplorationTask) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO exploration_tasks(query,reason,tools_json,status,created_at,expires_at,found,selected)
VALUES(?,?,?,?,?,?,?,?)`, t.Query, t.Reason, encode(t.Tools), t.Status, t.CreatedAt, t.ExpiresAt, t.Found, t.Selected)
	return err
}

func (s *Store) RecentTasks(ctx context.Context, since time.Time) ([]model.ExplorationTask, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,query,reason,tools_json,status,created_at,expires_at,found,selected FROM exploration_tasks WHERE created_at >= ? ORDER BY created_at DESC LIMIT 20`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ExplorationTask
	for rows.Next() {
		var t model.ExplorationTask
		var tools string
		if err := rows.Scan(&t.ID, &t.Query, &t.Reason, &tools, &t.Status, &t.CreatedAt, &t.ExpiresAt, &t.Found, &t.Selected); err != nil {
			return nil, err
		}
		t.Tools = decode(tools)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) SetSetting(ctx context.Context, k, v string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v)
	return err
}

func (s *Store) Setting(ctx context.Context, k string) (string, bool) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, k).Scan(&v)
	return v, err == nil
}

func scanItems(rows *sql.Rows) ([]model.Item, error) {
	var out []model.Item
	for rows.Next() {
		var it model.Item
		var tags string
		var imageURL sql.NullString
		var pub sql.NullTime
		if err := rows.Scan(&it.ID, &it.Source, &it.Title, &it.URL, &imageURL, &it.Author, &it.Content, &it.Summary, &pub, &it.FetchedAt, &it.Hash, &tags, &it.Score, &it.Reason, &it.Status); err != nil {
			return nil, err
		}
		if imageURL.Valid {
			it.ImageURL = imageURL.String
		}
		if pub.Valid {
			it.PublishedAt = pub.Time
		}
		it.Tags = decode(tags)
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) ensureColumn(table, column, def string) error {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + def)
	return err
}

func encode(v []string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func decode(s string) []string {
	var out []string
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func powHalf(x float64) float64 {
	if x <= 0 {
		return 1
	}
	return math.Pow(0.5, x)
}

func NormalizeTopic(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func (s *Store) DumpStatus(ctx context.Context) string {
	var items, topics, feedback int
	_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM items`).Scan(&items)
	_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM topics`).Scan(&topics)
	_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM feedback`).Scan(&feedback)
	return fmt.Sprintf("items=%d topics=%d feedback=%d", items, topics, feedback)
}

func (s *Store) Backup(ctx context.Context, dir string) (string, error) {
	if s.path == ":memory:" {
		return "", fmt.Errorf("cannot back up in-memory database")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "radar-"+time.Now().Format("20060102-150405")+".db")
	escaped := strings.ReplaceAll(dst, "'", "''")
	if _, err := s.db.ExecContext(ctx, "VACUUM main INTO '"+escaped+"'"); err != nil {
		return "", err
	}
	return dst, nil
}

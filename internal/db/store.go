package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db   *sql.DB
	path string
}

type Note struct {
	ID        int64
	Text      string
	CreatedAt time.Time
}

type Todo struct {
	ID        int64
	Text      string
	Done      bool
	CreatedAt time.Time
	DoneAt    *time.Time
}

type Reminder struct {
	ID        int64
	Text      string
	DueAt     time.Time
	SentAt    *time.Time
	CreatedAt time.Time
}

type AIEvent struct {
	ID          int64
	Source      string
	Kind        string
	Level       string
	Title       string
	Body        string
	Context     string
	Options     []string
	ExternalID  string
	Status      string
	Response    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	RespondedAt *time.Time
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
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
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS notes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  text TEXT NOT NULL,
  created_at DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS todos (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  text TEXT NOT NULL,
  done INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL,
  done_at DATETIME
);
CREATE TABLE IF NOT EXISTS reminders (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  text TEXT NOT NULL,
  due_at DATETIME NOT NULL,
  sent_at DATETIME,
  created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_todos_done ON todos(done, id);
CREATE INDEX IF NOT EXISTS idx_reminders_due ON reminders(sent_at, due_at);
CREATE TABLE IF NOT EXISTS ai_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source TEXT NOT NULL,
  kind TEXT NOT NULL,
  level TEXT NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  context TEXT,
  options_json TEXT NOT NULL DEFAULT '[]',
  external_id TEXT,
  status TEXT NOT NULL,
  response TEXT,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  responded_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_ai_events_status ON ai_events(status, id);
CREATE INDEX IF NOT EXISTS idx_ai_events_external ON ai_events(source, external_id);
`)
	return err
}

func (s *Store) Setting(ctx context.Context, k string) (string, bool) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, k).Scan(&v)
	return v, err == nil
}

func (s *Store) SetSetting(ctx context.Context, k, v string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v)
	return err
}

func (s *Store) AddNote(ctx context.Context, text string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO notes(text,created_at) VALUES(?,?)`, text, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) Notes(ctx context.Context, limit int) ([]Note, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,text,created_at FROM notes ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Note
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.Text, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) AddTodo(ctx context.Context, text string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO todos(text,created_at) VALUES(?,?)`, text, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) Todos(ctx context.Context) ([]Todo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,text,done,created_at,done_at FROM todos WHERE done=0 ORDER BY id LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Todo
	for rows.Next() {
		var t Todo
		var doneAt sql.NullTime
		if err := rows.Scan(&t.ID, &t.Text, &t.Done, &t.CreatedAt, &doneAt); err != nil {
			return nil, err
		}
		if doneAt.Valid {
			t.DoneAt = &doneAt.Time
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) DoneTodo(ctx context.Context, id int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE todos SET done=1, done_at=? WHERE id=? AND done=0`, time.Now(), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) AddReminder(ctx context.Context, text string, dueAt time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO reminders(text,due_at,created_at) VALUES(?,?,?)`, text, dueAt, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) DueReminders(ctx context.Context, now time.Time) ([]Reminder, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,text,due_at,sent_at,created_at FROM reminders WHERE sent_at IS NULL AND due_at <= ? ORDER BY due_at LIMIT 20`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanReminders(rows)
}

func (s *Store) UpcomingReminders(ctx context.Context, now time.Time) ([]Reminder, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,text,due_at,sent_at,created_at FROM reminders WHERE sent_at IS NULL AND due_at > ? ORDER BY due_at LIMIT 10`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanReminders(rows)
}

func (s *Store) MarkReminderSent(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE reminders SET sent_at=? WHERE id=? AND sent_at IS NULL`, time.Now(), id)
	return err
}

func (s *Store) AddAIEvent(ctx context.Context, ev AIEvent) (int64, error) {
	now := time.Now()
	if ev.Source == "" {
		ev.Source = "unknown"
	}
	if ev.Kind == "" {
		ev.Kind = "notify"
	}
	if ev.Level == "" {
		ev.Level = "info"
	}
	if ev.Status == "" {
		if ev.Kind == "ask" {
			ev.Status = "pending"
		} else {
			ev.Status = "notified"
		}
	}
	options, _ := json.Marshal(ev.Options)
	res, err := s.db.ExecContext(ctx, `INSERT INTO ai_events(source,kind,level,title,body,context,options_json,external_id,status,response,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, ev.Source, ev.Kind, ev.Level, ev.Title, ev.Body, ev.Context, string(options), ev.ExternalID, ev.Status, ev.Response, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) AIEvent(ctx context.Context, id int64) (AIEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,source,kind,level,title,body,context,options_json,external_id,status,response,created_at,updated_at,responded_at FROM ai_events WHERE id=?`, id)
	if err != nil {
		return AIEvent{}, err
	}
	defer rows.Close()
	events, err := scanAIEvents(rows)
	if err != nil {
		return AIEvent{}, err
	}
	if len(events) == 0 {
		return AIEvent{}, sql.ErrNoRows
	}
	return events[0], nil
}

func (s *Store) RecentAIEvents(ctx context.Context, limit int) ([]AIEvent, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,source,kind,level,title,body,context,options_json,external_id,status,response,created_at,updated_at,responded_at FROM ai_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAIEvents(rows)
}

func (s *Store) SetAIEventResponse(ctx context.Context, id int64, status, response string) (bool, error) {
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `UPDATE ai_events SET status=?, response=?, updated_at=?, responded_at=? WHERE id=?`, status, response, now, now, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) Status(ctx context.Context) string {
	var notes, todos, reminders, pendingAI int
	_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM notes`).Scan(&notes)
	_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM todos WHERE done=0`).Scan(&todos)
	_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM reminders WHERE sent_at IS NULL`).Scan(&reminders)
	_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM ai_events WHERE status='pending'`).Scan(&pendingAI)
	return fmt.Sprintf("notes=%d open_todos=%d pending_reminders=%d pending_ai=%d", notes, todos, reminders, pendingAI)
}

func (s *Store) Backup(ctx context.Context, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "meowbot-"+time.Now().Format("20060102-150405")+".db")
	escaped := strings.ReplaceAll(dst, "'", "''")
	if _, err := s.db.ExecContext(ctx, "VACUUM main INTO '"+escaped+"'"); err != nil {
		return "", err
	}
	return dst, nil
}

func scanReminders(rows *sql.Rows) ([]Reminder, error) {
	var out []Reminder
	for rows.Next() {
		var r Reminder
		var sentAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.Text, &r.DueAt, &sentAt, &r.CreatedAt); err != nil {
			return nil, err
		}
		if sentAt.Valid {
			r.SentAt = &sentAt.Time
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanAIEvents(rows *sql.Rows) ([]AIEvent, error) {
	var out []AIEvent
	for rows.Next() {
		var ev AIEvent
		var context, externalID, response sql.NullString
		var options string
		var respondedAt sql.NullTime
		if err := rows.Scan(&ev.ID, &ev.Source, &ev.Kind, &ev.Level, &ev.Title, &ev.Body, &context, &options, &externalID, &ev.Status, &response, &ev.CreatedAt, &ev.UpdatedAt, &respondedAt); err != nil {
			return nil, err
		}
		if context.Valid {
			ev.Context = context.String
		}
		if externalID.Valid {
			ev.ExternalID = externalID.String
		}
		if response.Valid {
			ev.Response = response.String
		}
		if respondedAt.Valid {
			ev.RespondedAt = &respondedAt.Time
		}
		_ = json.Unmarshal([]byte(options), &ev.Options)
		out = append(out, ev)
	}
	return out, rows.Err()
}

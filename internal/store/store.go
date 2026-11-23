// Package store keeps users, documents, token usage and the query log in SQLite.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")
var ErrExists = errors.New("already exists")

const schema = `
CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'user',
    token_budget  INTEGER NOT NULL,
    created_at    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS documents (
    id         TEXT PRIMARY KEY,
    owner      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    format     TEXT NOT NULL,
    size       INTEGER NOT NULL,
    status     TEXT NOT NULL,
    error      TEXT NOT NULL DEFAULT '',
    chunks     INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS documents_owner ON documents(owner, created_at);
CREATE TABLE IF NOT EXISTS queries (
    id             TEXT PRIMARY KEY,
    owner          TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    question       TEXT NOT NULL,
    answer         TEXT NOT NULL,
    risk           TEXT NOT NULL,
    grounded       REAL NOT NULL,
    confidence     REAL NOT NULL,
    top_sources    TEXT NOT NULL,
    tokens         INTEGER NOT NULL,
    latency_ms     INTEGER NOT NULL,
    feedback       INTEGER NOT NULL DEFAULT 0,
    created_at     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS queries_owner ON queries(owner, created_at);
CREATE TABLE IF NOT EXISTS usage (
    owner      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    month      TEXT NOT NULL,
    tokens     INTEGER NOT NULL,
    requests   INTEGER NOT NULL,
    PRIMARY KEY (owner, month)
);`

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func NewID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func now() int64 { return time.Now().UnixMilli() }

// Month is the usage period key, e.g. "2026-11" (UTC).
func Month(t time.Time) string { return t.UTC().Format("2006-01") }

// ---- users ----

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	TokenBudget  int64     `json:"token_budget"` // per calendar month
	CreatedAt    time.Time `json:"created_at"`
}

func (s *Store) CreateUser(ctx context.Context, u *User) error {
	u.ID = NewID("u_")
	ts := now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, role, token_budget, created_at) VALUES (?,?,?,?,?,?)`,
		u.ID, u.Email, u.PasswordHash, u.Role, u.TokenBudget, ts)
	if err != nil {
		if isUnique(err) {
			return ErrExists
		}
		return err
	}
	u.CreatedAt = time.UnixMilli(ts).UTC()
	return nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) userBy(ctx context.Context, col, val string) (*User, error) {
	var u User
	var ts int64
	err := s.db.QueryRowContext(ctx, `SELECT id, email, password_hash, role, token_budget, created_at FROM users WHERE `+col+` = ?`, val).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.TokenBudget, &ts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	u.CreatedAt = time.UnixMilli(ts).UTC()
	return &u, err
}

func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	return s.userBy(ctx, "email", email)
}
func (s *Store) UserByID(ctx context.Context, id string) (*User, error) {
	return s.userBy(ctx, "id", id)
}

func (s *Store) SetBudget(ctx context.Context, id string, tokens int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET token_budget = ? WHERE id = ?`, tokens, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- documents ----

const (
	DocProcessing = "processing"
	DocReady      = "ready"
	DocFailed     = "failed"
)

type Document struct {
	ID        string    `json:"id"`
	Owner     string    `json:"-"`
	Name      string    `json:"name"`
	Format    string    `json:"format"`
	Size      int64     `json:"size"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	Chunks    int       `json:"chunks"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) CreateDocument(ctx context.Context, d *Document) error {
	d.ID = NewID("d_")
	d.Status = DocProcessing
	ts := now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO documents (id, owner, name, format, size, status, created_at) VALUES (?,?,?,?,?,?,?)`,
		d.ID, d.Owner, d.Name, d.Format, d.Size, d.Status, ts)
	d.CreatedAt = time.UnixMilli(ts).UTC()
	return err
}

func (s *Store) FinishDocument(ctx context.Context, id string, chunks int, failure error) error {
	status, msg := DocReady, ""
	if failure != nil {
		status, msg = DocFailed, failure.Error()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE documents SET status = ?, error = ?, chunks = ? WHERE id = ?`, status, msg, chunks, id)
	return err
}

func (s *Store) Documents(ctx context.Context, owner string) ([]Document, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, owner, name, format, size, status, error, chunks, created_at
		FROM documents WHERE owner = ? ORDER BY created_at DESC`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Document{}
	for rows.Next() {
		var d Document
		var ts int64
		if err := rows.Scan(&d.ID, &d.Owner, &d.Name, &d.Format, &d.Size, &d.Status, &d.Error, &d.Chunks, &ts); err != nil {
			return nil, err
		}
		d.CreatedAt = time.UnixMilli(ts).UTC()
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) DeleteDocument(ctx context.Context, owner, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM documents WHERE owner = ? AND id = ?`, owner, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- usage ----

type MonthUsage struct {
	Month    string `json:"month"`
	Tokens   int64  `json:"tokens"`
	Requests int64  `json:"requests"`
	Budget   int64  `json:"budget"`
}

func (s *Store) Usage(ctx context.Context, owner string, at time.Time) (MonthUsage, error) {
	u := MonthUsage{Month: Month(at)}
	err := s.db.QueryRowContext(ctx, `SELECT tokens, requests FROM usage WHERE owner = ? AND month = ?`, owner, u.Month).
		Scan(&u.Tokens, &u.Requests)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return u, err
}

func (s *Store) AddUsage(ctx context.Context, owner string, at time.Time, tokens int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO usage (owner, month, tokens, requests) VALUES (?,?,?,1)
		ON CONFLICT(owner, month) DO UPDATE SET tokens = tokens + excluded.tokens, requests = requests + 1`,
		owner, Month(at), tokens)
	return err
}

// ---- query log ----

type Query struct {
	ID         string    `json:"id"`
	Owner      string    `json:"-"`
	Question   string    `json:"question"`
	Answer     string    `json:"answer"`
	Risk       string    `json:"risk"`
	Grounded   float64   `json:"grounded"`
	Confidence float64   `json:"confidence"`
	TopSources string    `json:"top_sources"`
	Tokens     int64     `json:"tokens"`
	LatencyMs  int64     `json:"latency_ms"`
	Feedback   int       `json:"feedback"` // -1 unhelpful, 0 none, 1 helpful
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Store) LogQuery(ctx context.Context, q *Query) error {
	q.ID = NewID("q_")
	ts := now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO queries (id, owner, question, answer, risk, grounded, confidence, top_sources,
		tokens, latency_ms, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		q.ID, q.Owner, q.Question, q.Answer, q.Risk, q.Grounded, q.Confidence, q.TopSources, q.Tokens, q.LatencyMs, ts)
	q.CreatedAt = time.UnixMilli(ts).UTC()
	return err
}

func (s *Store) SetFeedback(ctx context.Context, owner, id string, v int) error {
	res, err := s.db.ExecContext(ctx, `UPDATE queries SET feedback = ? WHERE owner = ? AND id = ?`, v, owner, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Queries lists recent queries. flagged keeps only the ones worth reviewing:
// medium/high risk, low retrieval confidence, or marked unhelpful.
func (s *Store) Queries(ctx context.Context, owner string, flagged bool, limit int) ([]Query, error) {
	where := `owner = ?`
	if flagged {
		where += ` AND (risk != 'low' OR (confidence >= 0 AND confidence < 0.2) OR feedback < 0)`
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, owner, question, answer, risk, grounded, confidence, top_sources, tokens,
		latency_ms, feedback, created_at FROM queries WHERE `+where+` ORDER BY created_at DESC LIMIT ?`, owner, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Query{}
	for rows.Next() {
		var q Query
		var ts int64
		if err := rows.Scan(&q.ID, &q.Owner, &q.Question, &q.Answer, &q.Risk, &q.Grounded, &q.Confidence, &q.TopSources,
			&q.Tokens, &q.LatencyMs, &q.Feedback, &ts); err != nil {
			return nil, err
		}
		q.CreatedAt = time.UnixMilli(ts).UTC()
		out = append(out, q)
	}
	return out, rows.Err()
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

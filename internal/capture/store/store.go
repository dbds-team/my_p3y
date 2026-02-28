package store

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Record struct {
	ID         int64     `json:"id"`
	URL        string    `json:"url"`
	Path       string    `json:"path"`
	GetParams  string    `json:"get_params"`
	PostParams string    `json:"post_params"`
	UniqueKey  string    `json:"unique_key"`
	CreatedAt  time.Time `json:"created_at"`
}

type SQLiteStore struct {
	dbPath string
	mu     sync.RWMutex
	seen   map[string]struct{}
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	s := &SQLiteStore{dbPath: path, seen: make(map[string]struct{})}
	if err := s.initSchema(); err != nil {
		return nil, err
	}
	if err := s.loadSeenIndex(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SQLiteStore) Close() error {
	return nil
}

func (s *SQLiteStore) initSchema() error {
	query := `CREATE TABLE IF NOT EXISTS captured_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		url TEXT NOT NULL,
		path TEXT NOT NULL,
		get_params TEXT NOT NULL,
		post_params TEXT NOT NULL,
		unique_key TEXT NOT NULL,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
	);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_capture_unique_key ON captured_requests(unique_key);
	CREATE INDEX IF NOT EXISTS idx_capture_path ON captured_requests(path);
	CREATE INDEX IF NOT EXISTS idx_capture_created_at ON captured_requests(created_at DESC);`
	_, err := s.runSQL(query)
	if err != nil {
		return fmt.Errorf("init schema: %w", err)
	}
	return nil
}

func (s *SQLiteStore) loadSeenIndex() error {
	type row struct {
		UniqueKey string `json:"unique_key"`
	}

	out, err := s.runSQLJSON(`SELECT unique_key FROM captured_requests`)
	if err != nil {
		return fmt.Errorf("load seen index: %w", err)
	}

	var rows []row
	if err := json.Unmarshal(out, &rows); err != nil {
		return fmt.Errorf("unmarshal seen index: %w", err)
	}

	tmp := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		tmp[r.UniqueKey] = struct{}{}
	}

	s.mu.Lock()
	s.seen = tmp
	s.mu.Unlock()
	return nil
}

func (s *SQLiteStore) SeenBefore(key string) bool {
	s.mu.RLock()
	_, ok := s.seen[key]
	s.mu.RUnlock()
	return ok
}

func (s *SQLiteStore) Save(record Record, keepLatest int) error {
	if keepLatest <= 0 {
		keepLatest = 10
	}

	query := fmt.Sprintf(`BEGIN;
	INSERT INTO captured_requests (url, path, get_params, post_params, unique_key)
	VALUES (%s, %s, %s, %s, %s);
	DELETE FROM captured_requests
	WHERE path = %s
	  AND id NOT IN (
		SELECT id FROM captured_requests
		WHERE path = %s
		ORDER BY created_at DESC, id DESC
		LIMIT %d
	  );
	COMMIT;`,
		sqlQuote(record.URL),
		sqlQuote(record.Path),
		sqlQuote(record.GetParams),
		sqlQuote(record.PostParams),
		sqlQuote(record.UniqueKey),
		sqlQuote(record.Path),
		sqlQuote(record.Path),
		keepLatest,
	)

	if _, err := s.runSQL(query); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil
		}
		return fmt.Errorf("save capture: %w", err)
	}

	s.mu.Lock()
	s.seen[record.UniqueKey] = struct{}{}
	s.mu.Unlock()
	return nil
}

func (s *SQLiteStore) List(limit, offset int, path string) ([]Record, error) {
	if limit <= 0 {
		limit = 30
	}
	if offset < 0 {
		offset = 0
	}

	where := ""
	if path != "" {
		where = " WHERE path = " + sqlQuote(path)
	}

	query := fmt.Sprintf(`SELECT id, url, path, get_params, post_params, unique_key, created_at
	FROM captured_requests%s
	ORDER BY created_at DESC, id DESC
	LIMIT %d OFFSET %d`, where, limit, offset)

	out, err := s.runSQLJSON(query)
	if err != nil {
		return nil, fmt.Errorf("list captures: %w", err)
	}

	var rows []map[string]any
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("unmarshal captures: %w", err)
	}

	records := make([]Record, 0, len(rows))
	for _, m := range rows {
		records = append(records, Record{
			ID:         asInt64(m["id"]),
			URL:        asString(m["url"]),
			Path:       asString(m["path"]),
			GetParams:  asString(m["get_params"]),
			PostParams: asString(m["post_params"]),
			UniqueKey:  asString(m["unique_key"]),
			CreatedAt:  time.Unix(asInt64(m["created_at"]), 0),
		})
	}
	return records, nil
}

func (s *SQLiteStore) runSQL(query string) ([]byte, error) {
	cmd := exec.Command("sqlite3", s.dbPath, query)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("sqlite3 exec failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (s *SQLiteStore) runSQLJSON(query string) ([]byte, error) {
	cmd := exec.Command("sqlite3", "-json", s.dbPath, query)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("sqlite3 json exec failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if len(out) == 0 {
		return []byte("[]"), nil
	}
	return out, nil
}

func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func asInt64(v any) int64 {
	switch vv := v.(type) {
	case float64:
		return int64(vv)
	case int64:
		return vv
	case int:
		return int64(vv)
	case string:
		n, _ := strconv.ParseInt(vv, 10, 64)
		return n
	default:
		return 0
	}
}

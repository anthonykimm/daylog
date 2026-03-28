package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/adrg/xdg"
	_ "modernc.org/sqlite"
)

const currentSchemaVersion = 5

type Task struct {
	ID        int
	Title     string
	Completed bool
	Hidden    bool
	CreatedAt time.Time
}

func openDB() (*sql.DB, error) {
	dbDir := filepath.Join(xdg.DataHome, "daylog")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}

	dbPath := filepath.Join(dbDir, "daylog.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}

	if version < 1 {
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS tasks (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				title TEXT NOT NULL,
				completed BOOLEAN NOT NULL DEFAULT 0,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			);
			PRAGMA user_version = 1;
		`)
		if err != nil {
			return err
		}
		version = 1
	}

	if version < 2 {
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS hidden_commits (
				hash TEXT PRIMARY KEY,
				hidden_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			);
			PRAGMA user_version = 2;
		`)
		if err != nil {
			return err
		}
		version = 2
	}

	if version < 3 {
		_, err := db.Exec(`
			ALTER TABLE tasks ADD COLUMN hidden BOOLEAN NOT NULL DEFAULT 0;
			PRAGMA user_version = 3;
		`)
		if err != nil {
			return err
		}
		version = 3
	}

	if version < 4 {
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS config (
				key TEXT PRIMARY KEY,
				value TEXT NOT NULL
			);
			PRAGMA user_version = 4;
		`)
		if err != nil {
			return err
		}
		version = 4
	}

	if version < 5 {
		_, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS task_links (
				task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
				source TEXT NOT NULL,
				external_id TEXT NOT NULL,
				external_key TEXT,
				PRIMARY KEY (task_id, source)
			);
			PRAGMA user_version = 5;
		`)
		if err != nil {
			return err
		}
		version = 5
	}

	if version > currentSchemaVersion {
		return fmt.Errorf("unknown schema version %d (expected <=%d)", version, currentSchemaVersion)
	}

	return nil
}

func addTask(db *sql.DB, title string) (Task, error) {
	now := time.Now()
	result, err := db.Exec(
		"INSERT INTO tasks (title, completed, created_at) VALUES (?, 0, ?)",
		title, now,
	)
	if err != nil {
		return Task{}, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return Task{}, err
	}

	return Task{
		ID:        int(id),
		Title:     title,
		Completed: false,
		CreatedAt: now,
	}, nil
}

func toggleTask(db *sql.DB, id int) error {
	_, err := db.Exec("UPDATE tasks SET completed = NOT completed WHERE id = ?", id)
	return err
}

func deleteTask(db *sql.DB, id int) error {
	_, err := db.Exec("DELETE FROM tasks WHERE id = ?", id)
	return err
}

func hideTask(db *sql.DB, id int) error {
	_, err := db.Exec("UPDATE tasks SET hidden = 1 WHERE id = ?", id)
	return err
}

func unhideTask(db *sql.DB, id int) error {
	_, err := db.Exec("UPDATE tasks SET hidden = 0 WHERE id = ?", id)
	return err
}

func loadTasksForDate(db *sql.DB, date time.Time) ([]Task, error) {
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	end := start.Add(24 * time.Hour)

	rows, err := db.Query(
		"SELECT id, title, completed, hidden, created_at FROM tasks WHERE created_at >= ? AND created_at < ? ORDER BY id",
		start, end,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.Title, &t.Completed, &t.Hidden, &t.CreatedAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func hideCommit(db *sql.DB, hash string) error {
	_, err := db.Exec("INSERT OR IGNORE INTO hidden_commits (hash) VALUES (?)", hash)
	return err
}

func unhideCommit(db *sql.DB, hash string) error {
	_, err := db.Exec("DELETE FROM hidden_commits WHERE hash = ?", hash)
	return err
}

func loadHiddenCommits(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query("SELECT hash FROM hidden_commits")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hidden := make(map[string]bool)
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, err
		}
		hidden[hash] = true
	}
	return hidden, rows.Err()
}

func getConfig(db *sql.DB, key string) (string, error) {
	var value string
	err := db.QueryRow("SELECT value FROM config WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func setConfig(db *sql.DB, key, value string) error {
	_, err := db.Exec(
		"INSERT INTO config (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = ?",
		key, value, value,
	)
	return err
}

func deleteConfig(db *sql.DB, key string) error {
	_, err := db.Exec("DELETE FROM config WHERE key = ?", key)
	return err
}

func addTaskWithLink(db *sql.DB, title, source, externalID, externalKey string) (Task, error) {
	now := time.Now()
	result, err := db.Exec(
		"INSERT INTO tasks (title, completed, created_at) VALUES (?, 0, ?)",
		title, now,
	)
	if err != nil {
		return Task{}, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return Task{}, err
	}

	_, err = db.Exec(
		"INSERT INTO task_links (task_id, source, external_id, external_key) VALUES (?, ?, ?, ?)",
		id, source, externalID, externalKey,
	)
	if err != nil {
		return Task{}, err
	}

	return Task{
		ID:        int(id),
		Title:     title,
		Completed: false,
		CreatedAt: now,
	}, nil
}

func getTaskLink(db *sql.DB, taskID int, source string) (externalID, externalKey string, err error) {
	err = db.QueryRow(
		"SELECT external_id, external_key FROM task_links WHERE task_id = ? AND source = ?",
		taskID, source,
	).Scan(&externalID, &externalKey)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return
}

func isLinearIssueLinked(db *sql.DB, externalID string) bool {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM task_links WHERE source = 'linear' AND external_id = ?",
		externalID,
	).Scan(&count)
	return err == nil && count > 0
}

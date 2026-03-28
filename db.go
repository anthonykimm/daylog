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

const currentSchemaVersion = 2

type Task struct {
	ID        int
	Title     string
	Completed bool
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

func loadTasksForDate(db *sql.DB, date time.Time) ([]Task, error) {
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	end := start.Add(24 * time.Hour)

	rows, err := db.Query(
		"SELECT id, title, completed, created_at FROM tasks WHERE created_at >= ? AND created_at < ? ORDER BY id",
		start, end,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.Title, &t.Completed, &t.CreatedAt); err != nil {
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

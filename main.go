package main

import (
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	db, err := openDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	tasks, err := loadTasksForDate(db, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading tasks: %v\n", err)
		os.Exit(1)
	}

	hidden, err := loadHiddenCommits(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading hidden commits: %v\n", err)
		os.Exit(1)
	}

	commits := loadCommits(hidden, false)

	p := tea.NewProgram(newModel(db, tasks, commits, hidden), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

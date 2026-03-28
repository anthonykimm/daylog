package main

import (
	"database/sql"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

var conventionalPrefix = regexp.MustCompile(`^(?:feat|fix|chore|docs|refactor|test|ci|build|perf|style)(?:\(.*?\))?:\s*`)

type mode int

const (
	modeNormal mode = iota
	modeInput
	modeLinearClientID
	modeLinearClientSecret
	modeLinearAuth
	modeLinearMenu
)

// doneItem represents an item in the Done pane's unified list.
type doneItem struct {
	isCommit bool
	task     Task   // populated if !isCommit
	commit   Commit // populated if isCommit
}

type model struct {
	db            *sql.DB
	tasks         []Task
	commits       []Commit
	linearIssues  []LinearIssue
	hiddenCommits map[string]bool
	showHidden    bool
	pane          int // 0 = task, 1 = done, 2 = linear
	taskCursor    int
	doneCursor    int
	linearCursor  int
	mode          mode
	input         textinput.Model
	linearInput   textinput.Model
	linearStatus  string
	linearMenuIdx int
	width         int
	height        int
	err           error
	quitting      bool
}

func newModel(db *sql.DB, tasks []Task, commits []Commit, hidden map[string]bool, issues []LinearIssue) model {
	ti := textinput.New()
	ti.Placeholder = "New task..."
	ti.CharLimit = 256

	li := textinput.New()
	li.CharLimit = 256

	return model{
		db:            db,
		tasks:         tasks,
		commits:       commits,
		linearIssues:  issues,
		hiddenCommits: hidden,
		input:         ti,
		linearInput:   li,
	}
}

func (m model) pendingTasks() []Task {
	var out []Task
	for _, t := range m.tasks {
		if !t.Completed {
			out = append(out, t)
		}
	}
	return out
}

func (m model) completedTasks() []Task {
	var out []Task
	for _, t := range m.tasks {
		if t.Completed && (!t.Hidden || m.showHidden) {
			out = append(out, t)
		}
	}
	return out
}

// doneItems builds the unified list for the Done pane:
// completed tasks, then visible commits.
func (m model) doneItems() []doneItem {
	var items []doneItem
	for _, t := range m.completedTasks() {
		items = append(items, doneItem{task: t})
	}
	for _, c := range m.commits {
		items = append(items, doneItem{isCommit: true, commit: c})
	}
	return items
}

func (m *model) resetLinearUI() {
	m.linearStatus = ""
	m.linearMenuIdx = 0
	m.linearInput.Reset()
	m.linearInput.Blur()
	m.linearInput.EchoMode = textinput.EchoNormal
}

func (m model) findTaskIndex(id int) int {
	for i, t := range m.tasks {
		if t.ID == id {
			return i
		}
	}
	return -1
}

func (m *model) refreshCommits() {
	m.commits = loadCommits(m.hiddenCommits, m.showHidden)
}

func (m *model) clampDoneCursor() {
	items := m.doneItems()
	if m.doneCursor >= len(items) {
		m.doneCursor = len(items) - 1
	}
	if m.doneCursor < 0 {
		m.doneCursor = 0
	}
}

// summaryPaneIndex returns the pane index for the Summary panel.
// 3 if Linear is present, 2 if not (but pane 2 is Linear when present).
func (m model) summaryPaneIndex() int {
	if linearIsAuthenticated(m.db) {
		return 3
	}
	return 2
}

// summaryItems builds the cleaned summary lines for display and clipboard.
func (m model) summaryItems() []string {
	var items []string
	linkedIDs := make(map[int]bool)

	// Completed tasks (non-hidden)
	for _, t := range m.tasks {
		if !t.Completed || t.Hidden {
			continue
		}
		// Check if linked to Linear
		_, extKey, _ := getTaskLink(m.db, t.ID, "linear")
		if extKey != "" {
			linkedIDs[t.ID] = true
			// Strip issue ID prefix: "ENG-123 - Subject" -> "Subject"
			title := t.Title
			if idx := strings.Index(title, " - "); idx != -1 {
				title = title[idx+3:]
			}
			items = append(items, title)
		} else {
			items = append(items, t.Title)
		}
	}

	// Git commits (non-hidden)
	for _, c := range m.commits {
		if c.Hidden {
			continue
		}
		subject := conventionalPrefix.ReplaceAllString(c.Subject, "")
		items = append(items, subject)
	}

	return items
}

// summaryText builds the full summary string for clipboard.
func (m model) summaryText() string {
	header := time.Now().Format("Jan 2") + " Update:"
	items := m.summaryItems()
	if len(items) == 0 {
		return header
	}
	var sb strings.Builder
	sb.WriteString(header)
	for _, item := range items {
		sb.WriteString("\n- " + item)
	}
	return sb.String()
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case linearAuthResult:
		if msg.err != nil {
			m.err = msg.err
			m.linearStatus = ""
		} else {
			m.linearStatus = "Connected to Linear"
		}
		m.mode = modeNormal
		// Fetch issues after successful auth
		if msg.err == nil {
			return m, m.fetchLinearIssues()
		}
		return m, nil
	case linearIssuesResult:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.linearIssues = msg.issues
		}
		return m, nil
	case linearMarkDoneResult:
		if msg.err != nil {
			m.err = msg.err
		}
		// Refresh issues after marking done
		return m, m.fetchLinearIssues()
	case tea.KeyMsg:
		switch m.mode {
		case modeInput:
			return m.handleInputMode(msg)
		case modeLinearClientID, modeLinearClientSecret:
			return m.handleLinearCredentialMode(msg)
		case modeLinearAuth:
			return m.handleLinearAuthMode(msg)
		case modeLinearMenu:
			return m.handleLinearMenuMode(msg)
		default:
			return m.handleNormalMode(msg)
		}
	}

	return m, nil
}

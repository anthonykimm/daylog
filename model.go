package main

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	borderColor    = lipgloss.Color("12")
	dimBorderColor = lipgloss.Color("8")
	selectedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	doneStyle      = lipgloss.NewStyle().Strikethrough(true).Foreground(lipgloss.Color("8"))
	commitStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	hiddenStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	dividerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	helpStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

type mode int

const (
	modeNormal mode = iota
	modeInput
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
	hiddenCommits map[string]bool
	showHidden    bool
	pane          int // 0 = task, 1 = done
	taskCursor    int
	doneCursor    int
	mode          mode
	input         textinput.Model
	width         int
	height        int
	err           error
	quitting      bool
}

func newModel(db *sql.DB, tasks []Task, commits []Commit, hidden map[string]bool) model {
	ti := textinput.New()
	ti.Placeholder = "New task..."
	ti.CharLimit = 256

	return model{
		db:            db,
		tasks:         tasks,
		commits:       commits,
		hiddenCommits: hidden,
		input:         ti,
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
		if t.Completed {
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

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		if m.mode == modeInput {
			return m.handleInputMode(msg)
		}
		return m.handleNormalMode(msg)
	}

	return m, nil
}

func (m model) handleNormalMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	pending := m.pendingTasks()
	items := m.doneItems()

	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit

	case "1":
		m.pane = 0

	case "2":
		m.pane = 1

	case "up", "k":
		if m.pane == 0 && m.taskCursor > 0 {
			m.taskCursor--
		} else if m.pane == 1 && m.doneCursor > 0 {
			m.doneCursor--
		}

	case "down", "j":
		if m.pane == 0 && m.taskCursor < len(pending)-1 {
			m.taskCursor++
		} else if m.pane == 1 && m.doneCursor < len(items)-1 {
			m.doneCursor++
		}

	case "a":
		m.pane = 0
		m.mode = modeInput
		m.input.Reset()
		m.input.Focus()
		return m, m.input.Cursor.BlinkCmd()

	case "r":
		m.refreshCommits()
		m.clampDoneCursor()

	case "u":
		if m.pane == 1 {
			m.showHidden = !m.showHidden
			m.refreshCommits()
			m.clampDoneCursor()
		}

	case " ", "enter":
		if m.pane == 0 && len(pending) > 0 {
			task := pending[m.taskCursor]
			idx := m.findTaskIndex(task.ID)
			if err := toggleTask(m.db, task.ID); err != nil {
				m.err = err
				return m, nil
			}
			m.tasks[idx].Completed = true
			if m.taskCursor >= len(pending)-1 && m.taskCursor > 0 {
				m.taskCursor--
			}
		} else if m.pane == 1 && len(items) > 0 {
			item := items[m.doneCursor]
			if item.isCommit {
				// Enter on a hidden commit restores it
				if item.commit.Hidden {
					if err := unhideCommit(m.db, item.commit.Hash); err != nil {
						m.err = err
						return m, nil
					}
					delete(m.hiddenCommits, item.commit.Hash)
					m.refreshCommits()
					m.clampDoneCursor()
				}
			} else {
				// Toggle completed task back to pending
				idx := m.findTaskIndex(item.task.ID)
				if err := toggleTask(m.db, item.task.ID); err != nil {
					m.err = err
					return m, nil
				}
				m.tasks[idx].Completed = false
				m.clampDoneCursor()
			}
		}

	case "d":
		if m.pane == 0 && len(pending) > 0 {
			task := pending[m.taskCursor]
			idx := m.findTaskIndex(task.ID)
			if err := deleteTask(m.db, task.ID); err != nil {
				m.err = err
				return m, nil
			}
			m.tasks = append(m.tasks[:idx], m.tasks[idx+1:]...)
			if m.taskCursor >= len(pending)-1 && m.taskCursor > 0 {
				m.taskCursor--
			}
		} else if m.pane == 1 && len(items) > 0 {
			item := items[m.doneCursor]
			if item.isCommit {
				if !item.commit.Hidden {
					if err := hideCommit(m.db, item.commit.Hash); err != nil {
						m.err = err
						return m, nil
					}
					m.hiddenCommits[item.commit.Hash] = true
					m.refreshCommits()
					m.clampDoneCursor()
				}
			} else {
				idx := m.findTaskIndex(item.task.ID)
				if err := deleteTask(m.db, item.task.ID); err != nil {
					m.err = err
					return m, nil
				}
				m.tasks = append(m.tasks[:idx], m.tasks[idx+1:]...)
				m.clampDoneCursor()
			}
		}
	}

	return m, nil
}

func (m model) handleInputMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		title := m.input.Value()
		if title != "" {
			task, err := addTask(m.db, title)
			if err != nil {
				m.err = err
				return m, nil
			}
			m.tasks = append(m.tasks, task)
			m.taskCursor = len(m.pendingTasks()) - 1
		}
		m.mode = modeNormal
		m.input.Blur()
		return m, nil

	case "esc":
		m.mode = modeNormal
		m.input.Blur()
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if m.quitting {
		return ""
	}

	pending := m.pendingTasks()
	items := m.doneItems()

	colWidth := m.width / 2
	panelHeight := m.height - 1 // 1 line for help bar

	// Build task column content
	var taskContent strings.Builder
	if len(pending) == 0 && m.mode != modeInput {
		taskContent.WriteString("  No tasks. Press 'a' to add.\n")
	}
	for i, task := range pending {
		cursor := "  "
		if m.pane == 0 && i == m.taskCursor {
			cursor = "> "
		}
		line := fmt.Sprintf("%s%s", cursor, task.Title)
		if m.pane == 0 && i == m.taskCursor {
			line = selectedStyle.Render(line)
		}
		taskContent.WriteString(line + "\n")
	}
	if m.mode == modeInput {
		taskContent.WriteString("\n  " + m.input.View() + "\n")
	}

	// Build done column content with unified item list
	var doneContent strings.Builder
	completedCount := len(m.completedTasks())
	inCommits := false

	if len(items) == 0 {
		doneContent.WriteString("  Nothing here yet.\n")
	}

	for i, item := range items {
		// Insert divider when transitioning from tasks to commits
		if item.isCommit && !inCommits {
			if completedCount > 0 {
				doneContent.WriteString(dividerStyle.Render("  ── commits ──") + "\n")
			} else {
				doneContent.WriteString(dividerStyle.Render("  ── commits ──") + "\n")
			}
			inCommits = true
		}

		cursor := "  "
		if m.pane == 1 && i == m.doneCursor {
			cursor = "> "
		}

		if item.isCommit {
			line := fmt.Sprintf("%s%s %s", cursor, item.commit.Hash, item.commit.Subject)
			if item.commit.Hidden {
				line = hiddenStyle.Render(line)
			} else if m.pane == 1 && i == m.doneCursor {
				line = selectedStyle.Render(line)
			} else {
				line = commitStyle.Render(line)
			}
			doneContent.WriteString(line + "\n")
		} else {
			line := fmt.Sprintf("%s%s", cursor, item.task.Title)
			if m.pane == 1 && i == m.doneCursor {
				line = selectedStyle.Render(line)
			} else {
				line = doneStyle.Render(line)
			}
			doneContent.WriteString(line + "\n")
		}
	}

	leftPanel := renderPanel("Task [1]", taskContent.String(), colWidth, panelHeight, m.pane == 0)
	rightPanel := renderPanel("Done [2]", doneContent.String(), m.width-colWidth, panelHeight, m.pane == 1)

	columns := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	help := helpStyle.Render(" a: add • space/enter: toggle • d: delete/hide • r: refresh • u: show hidden • j/k: nav • 1/2: pane • q: quit")

	if m.err != nil {
		help += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(fmt.Sprintf("Error: %v", m.err))
	}

	return columns + "\n" + help
}

func renderPanel(title string, content string, width, height int, focused bool) string {
	bc := dimBorderColor
	if focused {
		bc = borderColor
	}

	bStyle := lipgloss.NewStyle().Foreground(bc)
	tStyle := lipgloss.NewStyle().Foreground(bc).Bold(true)

	innerW := width - 2
	innerH := height - 2
	if innerW < 0 {
		innerW = 0
	}
	if innerH < 0 {
		innerH = 0
	}

	// Top border: ╭─ Title ──...──╮
	titleText := fmt.Sprintf(" %s ", title)
	titleLen := len([]rune(titleText))
	dashesAfter := innerW - 1 - titleLen
	if dashesAfter < 0 {
		dashesAfter = 0
	}
	top := bStyle.Render("╭─") + tStyle.Render(titleText) + bStyle.Render(strings.Repeat("─", dashesAfter)+"╮")

	// Bottom border: ╰──...──╯
	bottom := bStyle.Render("╰" + strings.Repeat("─", innerW) + "╯")

	// Split content into lines and pad to fill inner height
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	for len(lines) < innerH {
		lines = append(lines, "")
	}
	lines = lines[:innerH]

	var sb strings.Builder
	sb.WriteString(top + "\n")
	for _, line := range lines {
		lineW := lipgloss.Width(line)
		pad := innerW - lineW
		if pad < 0 {
			pad = 0
		}
		sb.WriteString(bStyle.Render("│") + line + strings.Repeat(" ", pad) + bStyle.Render("│") + "\n")
	}
	sb.WriteString(bottom)

	return sb.String()
}

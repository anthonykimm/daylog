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
	doneStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	commitStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	hiddenStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	dividerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	helpStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	priorityUrgent = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))  // red
	priorityHigh   = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))            // orange
	priorityMedium = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))             // yellow
	priorityLow    = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))             // green

	statusBacklog  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // dim
	statusTodo     = lipgloss.NewStyle().Foreground(lipgloss.Color("15")) // white
	statusStarted  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow
	statusReview   = lipgloss.NewStyle().Foreground(lipgloss.Color("12")) // blue

	unsyncedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))   // dim gray
	syncedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("135")) // purple
)

func priorityIcon(priority int) string {
	switch priority {
	case 1:
		return priorityUrgent.Render("!!!")
	case 2:
		return priorityHigh.Render("!!")
	case 3:
		return priorityMedium.Render("!")
	case 4:
		return priorityLow.Render("·")
	default:
		return " "
	}
}

func statusIcon(statusType string) string {
	switch statusType {
	case "backlog":
		return statusBacklog.Render("●")
	case "unstarted":
		return statusTodo.Render("●")
	case "started":
		return statusStarted.Render("●")
	default:
		return statusReview.Render("●")
	}
}

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

func (m model) Init() tea.Cmd {
	return nil
}

// linearAuthResult is sent when the OAuth flow completes.
type linearAuthResult struct {
	token string
	err   error
}

// linearIssuesResult is sent when issue fetching completes.
type linearIssuesResult struct {
	issues []LinearIssue
	err    error
}

// linearMarkDoneResult is sent when marking an issue done completes.
type linearMarkDoneResult struct {
	err error
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

	case "3":
		if linearIsAuthenticated(m.db) {
			m.pane = 2
		}

	case "up", "k":
		if m.pane == 0 && m.taskCursor > 0 {
			m.taskCursor--
		} else if m.pane == 1 && m.doneCursor > 0 {
			m.doneCursor--
		} else if m.pane == 2 && m.linearCursor > 0 {
			m.linearCursor--
		}

	case "down", "j":
		if m.pane == 0 && m.taskCursor < len(pending)-1 {
			m.taskCursor++
		} else if m.pane == 1 && m.doneCursor < len(items)-1 {
			m.doneCursor++
		} else if m.pane == 2 && m.linearCursor < len(m.linearIssues)-1 {
			m.linearCursor++
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
		if linearIsAuthenticated(m.db) {
			return m, m.fetchLinearIssues()
		}

	case "L":
		m.resetLinearUI()
		if linearIsAuthenticated(m.db) || linearHasCredentials(m.db) {
			m.mode = modeLinearMenu
		} else {
			m.mode = modeLinearClientID
			m.linearInput.Placeholder = "Linear Client ID"
			m.linearInput.Focus()
			return m, m.linearInput.Cursor.BlinkCmd()
		}

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
			// If linked to Linear, mark done there too
			externalID, _, _ := getTaskLink(m.db, task.ID, "linear")
			if externalID != "" {
				token := linearGetToken(m.db)
				if token != "" {
					return m, func() tea.Msg {
						err := linearMarkDone(token, externalID)
						return linearMarkDoneResult{err: err}
					}
				}
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
				idx := m.findTaskIndex(item.task.ID)
				if item.task.Hidden {
					// Unhide a hidden completed task
					if err := unhideTask(m.db, item.task.ID); err != nil {
						m.err = err
						return m, nil
					}
					m.tasks[idx].Hidden = false
				} else {
					// Toggle completed task back to pending
					if err := toggleTask(m.db, item.task.ID); err != nil {
						m.err = err
						return m, nil
					}
					m.tasks[idx].Completed = false
				}
				m.clampDoneCursor()
			}
		} else if m.pane == 2 && len(m.linearIssues) > 0 {
			issue := m.linearIssues[m.linearCursor]
			if !isLinearIssueLinked(m.db, issue.ID) {
				task, err := addTaskWithLink(m.db, issue.Key+" - "+issue.Title, "linear", issue.ID, issue.Key)
				if err != nil {
					m.err = err
					return m, nil
				}
				m.tasks = append(m.tasks, task)
			}
		}

	case "d":
		if m.pane == 0 && len(pending) > 0 {
			task := pending[m.taskCursor]
			idx := m.findTaskIndex(task.ID)
			deleteTaskLink(m.db, task.ID)
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
				if !item.task.Hidden {
					if err := hideTask(m.db, item.task.ID); err != nil {
						m.err = err
						return m, nil
					}
					m.tasks[idx].Hidden = true
				}
				m.clampDoneCursor()
			}
		}

	case "o":
		if m.pane == 2 && len(m.linearIssues) > 0 {
			issue := m.linearIssues[m.linearCursor]
			openBrowser(issue.URL)
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

func (m model) fetchLinearIssues() tea.Cmd {
	return func() tea.Msg {
		token := linearGetToken(m.db)
		if token == "" {
			return linearIssuesResult{}
		}
		issues, err := linearFetchIssues(token)
		return linearIssuesResult{issues: issues, err: err}
	}
}

func (m model) startLinearOAuth() tea.Cmd {
	return func() tea.Msg {
		token, err := linearStartOAuth(m.db)
		return linearAuthResult{token: token, err: err}
	}
}

func (m model) handleLinearCredentialMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.resetLinearUI()
		m.mode = modeNormal
		return m, nil

	case "enter":
		value := m.linearInput.Value()
		if value == "" {
			return m, nil
		}
		if m.mode == modeLinearClientID {
			// Store client ID temporarily, prompt for secret
			m.linearStatus = value // stash client ID temporarily
			m.mode = modeLinearClientSecret
			m.linearInput.Placeholder = "Linear Client Secret"
			m.linearInput.Reset()
			m.linearInput.Focus()
			m.linearInput.EchoMode = textinput.EchoPassword
			return m, m.linearInput.Cursor.BlinkCmd()
		}
		// modeLinearClientSecret — save both and start OAuth
		clientID := m.linearStatus
		clientSecret := value
		m.linearInput.Blur()
		m.linearInput.EchoMode = textinput.EchoNormal
		if err := linearSaveCredentials(m.db, clientID, clientSecret); err != nil {
			m.err = err
			m.mode = modeNormal
			return m, nil
		}
		m.mode = modeLinearAuth
		m.linearStatus = "Opening browser for Linear authorization..."
		return m, m.startLinearOAuth()
	}

	var cmd tea.Cmd
	m.linearInput, cmd = m.linearInput.Update(msg)
	return m, cmd
}

func (m model) handleLinearAuthMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While waiting for OAuth, only allow cancel
	if msg.String() == "esc" {
		m.resetLinearUI()
		m.mode = modeNormal
	}
	return m, nil
}

func (m model) linearMenuItems() []string {
	if linearIsAuthenticated(m.db) {
		return []string{"Re-authorize", "Reset credentials", "Disconnect"}
	}
	// Has credentials but no token
	return []string{"Authorize", "Reset credentials"}
}

func (m model) handleLinearMenuMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.linearMenuItems()

	switch msg.String() {
	case "esc":
		m.resetLinearUI()
		m.mode = modeNormal
		return m, nil

	case "up", "k":
		if m.linearMenuIdx > 0 {
			m.linearMenuIdx--
		}

	case "down", "j":
		if m.linearMenuIdx < len(items)-1 {
			m.linearMenuIdx++
		}

	case "enter":
		selected := items[m.linearMenuIdx]
		switch selected {
		case "Authorize", "Re-authorize":
			m.mode = modeLinearAuth
			m.linearStatus = "Opening browser for Linear authorization..."
			return m, m.startLinearOAuth()
		case "Reset credentials":
			if err := linearClearAll(m.db); err != nil {
				m.err = err
			}
			m.resetLinearUI()
			m.mode = modeLinearClientID
			m.linearInput.Placeholder = "Linear Client ID"
			m.linearInput.Focus()
			return m, m.linearInput.Cursor.BlinkCmd()
		case "Disconnect":
			if err := linearClearAll(m.db); err != nil {
				m.err = err
			}
			m.resetLinearUI()
			m.mode = modeNormal
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.quitting {
		return ""
	}

	pending := m.pendingTasks()
	items := m.doneItems()
	hasLinear := linearIsAuthenticated(m.db)

	// Render daylog banner at the top
	daylogBannerLines := []string{
		"░███████                         ░██",
		"░██   ░██                        ░██",
		"░██    ░██  ░██████   ░██    ░██ ░██  ░███████   ░████████",
		"░██    ░██       ░██  ░██    ░██ ░██ ░██    ░██ ░██    ░██",
		"░██    ░██  ░███████  ░██    ░██ ░██ ░██    ░██ ░██    ░██",
		"░██   ░██  ░██   ░██  ░██   ░███ ░██ ░██    ░██ ░██   ░███",
		"░███████    ░█████░██  ░█████░██ ░██  ░███████   ░█████░██",
		"                             ░██                       ░██",
		"                       ░███████                  ░███████",
	}
	daylogGradient := []string{"21", "27", "33", "39", "45", "51", "87", "123", "159"}
	var daylogBanner strings.Builder
	for i, line := range daylogBannerLines {
		colorIdx := i
		if colorIdx >= len(daylogGradient) {
			colorIdx = len(daylogGradient) - 1
		}
		style := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(daylogGradient[colorIdx]))
		daylogBanner.WriteString("  " + style.Render(line))
		if i < len(daylogBannerLines)-1 {
			daylogBanner.WriteString("\n")
		}
	}
	bannerHeight := len(daylogBannerLines) + 1 // +1 for spacing after

	isLinearOverlay := m.mode == modeLinearClientID || m.mode == modeLinearClientSecret || m.mode == modeLinearAuth || m.mode == modeLinearMenu

	colWidth := m.width / 2
	var availableHeight int
	if isLinearOverlay {
		availableHeight = m.height - 1
	} else {
		availableHeight = m.height - 1 - bannerHeight
	}
	var topHeight, bottomHeight int
	if hasLinear {
		bottomHeight = availableHeight / 3
		topHeight = availableHeight - bottomHeight
	} else {
		topHeight = availableHeight
	}

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
			if item.task.Hidden {
				line = hiddenStyle.Render(line)
			} else if m.pane == 1 && i == m.doneCursor {
				line = selectedStyle.Render(line)
			} else {
				line = doneStyle.Render(line)
			}
			doneContent.WriteString(line + "\n")
		}
	}

	leftPanel := renderPanel("Task [1]", taskContent.String(), colWidth, topHeight, m.pane == 0)
	rightPanel := renderPanel("Done [2]", doneContent.String(), m.width-colWidth, topHeight, m.pane == 1)

	columns := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	// Linear panel (bottom 1/3)
	if hasLinear {
		var linearContent strings.Builder
		if len(m.linearIssues) == 0 {
			linearContent.WriteString("  No assigned issues.\n")
		}
		for i, issue := range m.linearIssues {
			cursor := "  "
			if m.pane == 2 && i == m.linearCursor {
				cursor = "> "
			}

			linked := isLinearIssueLinked(m.db, issue.ID)
			var syncIcon string
			if linked {
				syncIcon = syncedStyle.Render("◆") + " "
			} else {
				syncIcon = unsyncedStyle.Render("◇") + " "
			}

			left := fmt.Sprintf("%s%s%s - %s", cursor, syncIcon, issue.Key, issue.Title)
			right := fmt.Sprintf("%s %s", priorityIcon(issue.Priority), statusIcon(issue.StatusType))

			// Pad to fill width, right-align the icons
			innerW := m.width - 4 // border + minimal padding
			leftW := lipgloss.Width(left)
			rightW := lipgloss.Width(right)
			gap := innerW - leftW - rightW
			if gap < 1 {
				gap = 1
			}

			line := left + strings.Repeat(" ", gap) + right

			if m.pane == 2 && i == m.linearCursor {
				line = selectedStyle.Render(line)
			}

			linearContent.WriteString(line + "\n")
		}

		linearPanel := renderPanel("Linear [3]", linearContent.String(), m.width, bottomHeight, m.pane == 2)
		columns = columns + "\n" + linearPanel
	}

	// Overlay for Linear auth modes
	bannerLines := []string{
		"░███████                         ░██                                        ░██                                          ",
		"░██   ░██                        ░██                            ░██ ░██     ░██                                          ",
		"░██    ░██  ░██████   ░██    ░██ ░██  ░███████   ░████████     ░██   ░██    ░██         ░██░████████   ░███████   ░██████   ░██░████ ",
		"░██    ░██       ░██  ░██    ░██ ░██ ░██    ░██ ░██    ░██    ░██     ░██   ░██         ░██░██    ░██ ░██    ░██       ░██  ░███     ",
		"░██    ░██  ░███████  ░██    ░██ ░██ ░██    ░██ ░██    ░██     ░██   ░██    ░██         ░██░██    ░██ ░█████████  ░███████  ░██      ",
		"░██   ░██  ░██   ░██  ░██   ░███ ░██ ░██    ░██ ░██   ░███      ░██ ░██     ░██         ░██░██    ░██ ░██        ░██   ░██  ░██      ",
		"░███████    ░█████░██  ░█████░██ ░██  ░███████   ░█████░██                  ░██████████ ░██░██    ░██  ░███████   ░█████░██ ░██      ",
		"                             ░██                       ░██                                                                           ",
		"                       ░███████                  ░███████                                                                            ",
	}
	// Pad all banner lines to the same width so they center as a block
	maxBannerWidth := 0
	for _, line := range bannerLines {
		w := lipgloss.Width(line)
		if w > maxBannerWidth {
			maxBannerWidth = w
		}
	}
	for i, line := range bannerLines {
		w := lipgloss.Width(line)
		if w < maxBannerWidth {
			bannerLines[i] = line + strings.Repeat(" ", maxBannerWidth-w)
		}
	}

	// Blue to light blue gradient: dark blue -> medium blue -> light blue
	gradientColors := []string{"21", "27", "33", "39", "45", "51", "87", "123", "159"}
	var linearBannerBuilder strings.Builder
	for i, line := range bannerLines {
		colorIdx := i
		if colorIdx >= len(gradientColors) {
			colorIdx = len(gradientColors) - 1
		}
		style := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(gradientColors[colorIdx]))
		linearBannerBuilder.WriteString(style.Render(line))
		if i < len(bannerLines)-1 {
			linearBannerBuilder.WriteString("\n")
		}
	}
	linearBanner := linearBannerBuilder.String()

	switch m.mode {
	case modeLinearClientID:
		body := fmt.Sprintf("Enter Linear Client ID:\n\n%s\n\n%s",
			m.linearInput.View(),
			helpStyle.Render("enter: confirm • esc: cancel"))
		columns = renderPanelWithBanner("Linear", linearBanner, body, m.width, availableHeight)
	case modeLinearClientSecret:
		body := fmt.Sprintf("Enter Linear Client Secret:\n\n%s\n\n%s",
			m.linearInput.View(),
			helpStyle.Render("enter: confirm • esc: cancel"))
		columns = renderPanelWithBanner("Linear", linearBanner, body, m.width, availableHeight)
	case modeLinearAuth:
		body := fmt.Sprintf("%s\n\n%s",
			m.linearStatus,
			helpStyle.Render("Waiting for browser... esc: cancel"))
		columns = renderPanelWithBanner("Linear", linearBanner, body, m.width, availableHeight)
	case modeLinearMenu:
		var menuBody strings.Builder
		for i, item := range m.linearMenuItems() {
			cursor := "  "
			if i == m.linearMenuIdx {
				cursor = "> "
			}
			line := fmt.Sprintf("%s%s", cursor, item)
			if i == m.linearMenuIdx {
				line = selectedStyle.Render(line)
			}
			menuBody.WriteString(line + "\n")
		}
		menuBody.WriteString("\n" + helpStyle.Render("enter: select • esc: cancel"))
		columns = renderPanelWithBanner("Linear", linearBanner, menuBody.String(), m.width, availableHeight)
	}

	// Build help bar
	var helpParts []string
	helpParts = append(helpParts, "a: add", "space/enter: toggle", "d: delete/hide", "r: refresh", "u: show hidden", "j/k: nav")
	if hasLinear {
		helpParts = append(helpParts, "o: open", "1/2/3: pane", "L: Linear")
	} else {
		helpParts = append(helpParts, "1/2: pane", "L: link Linear")
	}
	helpParts = append(helpParts, "q: quit")
	help := helpStyle.Render(" " + strings.Join(helpParts, " • "))

	if m.err != nil {
		help += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(fmt.Sprintf("Error: %v", m.err))
	}

	if isLinearOverlay {
		return columns + "\n" + help
	}
	return daylogBanner.String() + "\n\n" + columns + "\n" + help
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

func renderPanelCentered(title string, content string, width, height int, focused bool) string {
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

	// Split content into lines
	contentLines := strings.Split(strings.TrimRight(content, "\n"), "\n")

	// Vertical centering
	topPad := (innerH - len(contentLines)) / 2
	if topPad < 0 {
		topPad = 0
	}

	var lines []string
	for i := 0; i < topPad; i++ {
		lines = append(lines, "")
	}
	lines = append(lines, contentLines...)
	for len(lines) < innerH {
		lines = append(lines, "")
	}
	lines = lines[:innerH]

	var sb strings.Builder
	sb.WriteString(top + "\n")
	for _, line := range lines {
		lineW := lipgloss.Width(line)
		// Horizontal centering
		leftPad := (innerW - lineW) / 2
		rightPad := innerW - lineW - leftPad
		if leftPad < 0 {
			leftPad = 0
		}
		if rightPad < 0 {
			rightPad = 0
		}
		sb.WriteString(bStyle.Render("│") + strings.Repeat(" ", leftPad) + line + strings.Repeat(" ", rightPad) + bStyle.Render("│") + "\n")
	}
	sb.WriteString(bottom)

	return sb.String()
}

// renderPanelWithBanner renders a full-screen panel with a banner at the top
// and body content vertically centered in the remaining space.
func renderPanelWithBanner(title string, banner string, body string, width, height int) string {
	bc := borderColor
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

	// Top border
	titleText := fmt.Sprintf(" %s ", title)
	titleLen := len([]rune(titleText))
	dashesAfter := innerW - 1 - titleLen
	if dashesAfter < 0 {
		dashesAfter = 0
	}
	top := bStyle.Render("╭─") + tStyle.Render(titleText) + bStyle.Render(strings.Repeat("─", dashesAfter)+"╮")
	bottom := bStyle.Render("╰" + strings.Repeat("─", innerW) + "╯")

	// Banner lines (top-aligned, centered horizontally)
	bannerLines := strings.Split(strings.TrimRight(banner, "\n"), "\n")
	// Body lines (centered in the full panel height, independent of banner)
	bodyLines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	bodyTopPad := (innerH - len(bodyLines)) / 2
	if bodyTopPad < 0 {
		bodyTopPad = 0
	}

	// Start with empty panel
	allLines := make([]string, innerH)

	// Place banner at top with padding
	bannerStart := 10
	for i, line := range bannerLines {
		idx := bannerStart + i
		if idx < innerH {
			allLines[idx] = line
		}
	}

	// Place body centered vertically
	for i, line := range bodyLines {
		idx := bodyTopPad + i
		if idx < innerH {
			allLines[idx] = line
		}
	}

	var sb strings.Builder
	sb.WriteString(top + "\n")
	for _, line := range allLines {
		lineW := lipgloss.Width(line)
		leftPad := (innerW - lineW) / 2
		rightPad := innerW - lineW - leftPad
		if leftPad < 0 {
			leftPad = 0
		}
		if rightPad < 0 {
			rightPad = 0
		}
		sb.WriteString(bStyle.Render("│") + strings.Repeat(" ", leftPad) + line + strings.Repeat(" ", rightPad) + bStyle.Render("│") + "\n")
	}
	sb.WriteString(bottom)

	return sb.String()
}

package main

import (
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

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
		} else {
			m.pane = m.summaryPaneIndex()
		}

	case "4":
		if linearIsAuthenticated(m.db) {
			m.pane = m.summaryPaneIndex()
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
		if m.snapshot {
			break
		}
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
		if m.snapshot {
			break
		}
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
			m.refreshSummaryContent()
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
		if m.snapshot {
			break
		}
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

	case "c":
		if m.pane == m.summaryPaneIndex() {
			text := m.summaryText()
			copyToClipboard(text)
		}

	case "i":
		if m.pane == m.summaryPaneIndex() && !m.snapshot {
			m.mode = modeSummaryEdit
			m.summaryArea.Focus()
			return m, m.summaryArea.Cursor.BlinkCmd()
		}

	case "g":
		m.calendarDate = m.viewDate
		m.mode = modeCalendar

	case "esc":
		if m.snapshot {
			m.loadDataForDate(time.Now())
			m.snapshot = false
			m.pane = 0
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

func (m model) handleSummaryEditMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		m.summaryContent = m.summaryArea.Value()
		m.summaryEdited = true
		m.summaryArea.Blur()
		m.mode = modeNormal
		saveSummary(m.db, m.viewDate.Format("2006-01-02"), m.summaryContent)
		return m, nil
	}

	var cmd tea.Cmd
	m.summaryArea, cmd = m.summaryArea.Update(msg)
	return m, cmd
}

func (m model) handleCalendarMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeNormal
		return m, nil

	case "enter":
		m.loadDataForDate(m.calendarDate)
		m.mode = modeNormal
		m.pane = 0
		return m, nil

	case "h":
		m.calendarDate = m.calendarDate.AddDate(0, 0, -1)

	case "l":
		m.calendarDate = m.calendarDate.AddDate(0, 0, 1)

	case "k":
		m.calendarDate = m.calendarDate.AddDate(0, 0, -7)

	case "j":
		m.calendarDate = m.calendarDate.AddDate(0, 0, 7)

	case "H":
		m.calendarDate = m.calendarDate.AddDate(0, -1, 0)

	case "L":
		m.calendarDate = m.calendarDate.AddDate(0, 1, 0)
	}

	return m, nil
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

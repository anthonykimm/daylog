package main

import (
	"fmt"
	"strings"
	"time"

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

	statusBacklog = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // dim
	statusTodo    = lipgloss.NewStyle().Foreground(lipgloss.Color("15")) // white
	statusStarted = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow
	statusReview  = lipgloss.NewStyle().Foreground(lipgloss.Color("12")) // blue

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

func (m model) View() string {
	if m.quitting {
		return ""
	}

	pending := m.pendingTasks()
	items := m.doneItems()
	hasLinear := linearIsAuthenticated(m.db) && !m.snapshot

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
	daylogGradient := []string{"#4A6FA5", "#5A7FB0", "#6A8FBB", "#7A9FC6", "#8AAFD1", "#96B9D0", "#A0C1D4", "#AECBD6", "#B8D2DA"}
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

	// Build stats panel (right side of banner)
	remaining := len(pending)
	completed := len(m.completedTasks())
	commitCount := len(m.commits)
	total := remaining + completed
	var progressStr string
	if total > 0 {
		pct := completed * 100 / total
		progressStr = fmt.Sprintf("%d/%d (%d%%)", completed, total, pct)
	} else {
		progressStr = "0/0"
	}

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	dateStyle := lipgloss.NewStyle().Foreground(borderColor).Bold(true)

	dateStr := m.viewDate.Format("Mon, Jan 2")
	if m.snapshot {
		dateStr += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true).Render("(read-only)")
	}

	pad := "    "
	var statsBlock strings.Builder
	statsBlock.WriteString("\n")
	statsBlock.WriteString(pad + dateStyle.Render(dateStr) + "\n")
	statsBlock.WriteString("\n")
	statsBlock.WriteString(pad + labelStyle.Render("Remaining   ") + valueStyle.Render(fmt.Sprintf("%d", remaining)) + "\n")
	statsBlock.WriteString(pad + labelStyle.Render("Completed   ") + valueStyle.Render(fmt.Sprintf("%d", completed)) + "\n")
	statsBlock.WriteString(pad + labelStyle.Render("Commits     ") + valueStyle.Render(fmt.Sprintf("%d", commitCount)) + "\n")
	statsBlock.WriteString(pad + labelStyle.Render("Progress    ") + valueStyle.Render(progressStr) + "\n")

	// Pad stats block to match banner height
	statsLines := strings.Split(strings.TrimRight(statsBlock.String(), "\n"), "\n")
	for len(statsLines) < len(daylogBannerLines) {
		statsLines = append(statsLines, "")
	}
	paddedStats := strings.Join(statsLines[:len(daylogBannerLines)], "\n")

	// Join banner and stats horizontally, right-align stats
	bannerStr := daylogBanner.String()
	bannerW := 0
	for _, line := range daylogBannerLines {
		w := lipgloss.Width("  " + line)
		if w > bannerW {
			bannerW = w
		}
	}
	statsW := 0
	for _, line := range statsLines {
		w := lipgloss.Width(line)
		if w > statsW {
			statsW = w
		}
	}
	gap := m.width - bannerW - statsW - 4 // 4 for right margin
	if gap < 2 {
		gap = 2
	}
	headerBlock := lipgloss.JoinHorizontal(lipgloss.Top, "  "+paddedStats, strings.Repeat(" ", gap), bannerStr)

	isOverlay := m.mode == modeLinearClientID || m.mode == modeLinearClientSecret || m.mode == modeLinearAuth || m.mode == modeLinearMenu || m.mode == modeCalendar

	colWidth := m.width / 2
	var availableHeight int
	if isOverlay {
		availableHeight = m.height - 1
	} else {
		availableHeight = m.height - 1 - bannerHeight
	}
	bottomHeight := availableHeight / 3
	topHeight := availableHeight - bottomHeight

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

	// Build Summary panel content
	summaryPane := m.summaryPaneIndex()
	summaryHeader := time.Now().Format("Jan 2") + " Update:"
	var summaryContent strings.Builder
	summaryItems := m.summaryItems()
	summaryContent.WriteString("  " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Render(summaryHeader) + "\n\n")
	if len(summaryItems) == 0 {
		summaryContent.WriteString("  No completed items yet.\n")
	}
	for _, item := range summaryItems {
		summaryContent.WriteString("  " + helpStyle.Render("- ") + item + "\n")
	}

	// Bottom row
	if hasLinear {
		// Linear panel (bottom left)
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
			innerW := colWidth - 4
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

		summaryTitle := fmt.Sprintf("Summary [%d]", summaryPane+1)
		linearPanel := renderPanel("Linear [3]", linearContent.String(), colWidth, bottomHeight, m.pane == 2)
		summaryPanel := renderPanel(summaryTitle, summaryContent.String(), m.width-colWidth, bottomHeight, m.pane == summaryPane)
		bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, linearPanel, summaryPanel)
		columns = columns + "\n" + bottomRow
	} else {
		// Summary only (full width)
		summaryTitle := fmt.Sprintf("Summary [%d]", summaryPane+1)
		summaryPanel := renderPanel(summaryTitle, summaryContent.String(), m.width, bottomHeight, m.pane == summaryPane)
		columns = columns + "\n" + summaryPanel
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
	gradientColors := []string{"#4A6FA5", "#5A7FB0", "#6A8FBB", "#7A9FC6", "#8AAFD1", "#96B9D0", "#A0C1D4", "#AECBD6", "#B8D2DA"}
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
	case modeCalendar:
		calBody := m.renderCalendar()
		columns = renderPanelCentered("Go to Date", calBody, m.width, availableHeight, true)
	}

	// Build help bar
	var helpParts []string
	helpParts = append(helpParts, "a: add", "space/enter: toggle", "d: delete/hide", "r: refresh", "u: show hidden", "c: copy", "g: go to date", "j/k: nav")
	if hasLinear {
		helpParts = append(helpParts, "o: open", "1/2/3/4: pane", "L: Linear")
	} else {
		helpParts = append(helpParts, "1/2/3: pane", "L: link Linear")
	}
	if m.snapshot {
		helpParts = append(helpParts, "esc: back to today")
	}
	helpParts = append(helpParts, "q: quit")
	help := helpStyle.Render(" " + strings.Join(helpParts, " • "))

	if m.err != nil {
		help += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(fmt.Sprintf("Error: %v", m.err))
	}

	if isOverlay {
		return columns + "\n" + help
	}
	return headerBlock + "\n\n" + columns + "\n" + help
}

func (m model) renderCalendar() string {
	cd := m.calendarDate
	year, month, _ := cd.Date()
	loc := cd.Location()
	now := time.Now()

	// Header
	header := lipgloss.NewStyle().Bold(true).Foreground(borderColor).Render(
		fmt.Sprintf("%s %d", month.String(), year),
	)

	// Day of week header
	dowHeader := helpStyle.Render("Mo Tu We Th Fr Sa Su")

	// Find days with activity
	active := m.daysWithActivity(year, month)

	// First day of month
	first := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	// Weekday offset (Monday = 0)
	offset := int(first.Weekday())
	if offset == 0 {
		offset = 6 // Sunday
	} else {
		offset--
	}
	daysInMonth := time.Date(year, month+1, 0, 0, 0, 0, 0, loc).Day()

	selectedDay := cd.Day()
	todayDay := -1
	if now.Year() == year && now.Month() == month {
		todayDay = now.Day()
	}

	var grid strings.Builder
	// Pad first week
	for i := 0; i < offset; i++ {
		grid.WriteString("   ")
	}

	for day := 1; day <= daysInMonth; day++ {
		dayStr := fmt.Sprintf("%2d", day)

		isFuture := time.Date(year, month, day, 23, 59, 59, 0, loc).After(now)

		if day == selectedDay {
			// Cursor
			dayStr = lipgloss.NewStyle().Bold(true).Reverse(true).Foreground(borderColor).Render(dayStr)
		} else if day == todayDay {
			dayStr = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14")).Render(dayStr)
		} else if isFuture {
			dayStr = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(dayStr)
		} else if active[day] {
			dayStr = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Render(dayStr)
		} else {
			dayStr = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(dayStr)
		}

		grid.WriteString(dayStr)

		col := (offset + day - 1) % 7
		if col == 6 {
			// Pad line to full grid width (20 visual chars for "Mo Tu We Th Fr Sa Su")
			lineW := lipgloss.Width(grid.String()[strings.LastIndex(grid.String(), "\n")+1:])
			if lineW < 20 {
				grid.WriteString(strings.Repeat(" ", 20-lineW))
			}
			if day < daysInMonth {
				grid.WriteString("\n")
			}
		} else if day < daysInMonth {
			grid.WriteString(" ")
		}
	}

	// Pad the last line to full width
	lastNewline := strings.LastIndex(grid.String(), "\n")
	var lastLine string
	if lastNewline == -1 {
		lastLine = grid.String()
	} else {
		lastLine = grid.String()[lastNewline+1:]
	}
	lastLineW := lipgloss.Width(lastLine)
	if lastLineW < 20 {
		grid.WriteString(strings.Repeat(" ", 20-lastLineW))
	}

	help := helpStyle.Render("h/l: day • j/k: week • H/L: month • enter: select • esc: cancel")

	return fmt.Sprintf("%s\n%s\n%s\n\n%s", header, dowHeader, grid.String(), help)
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
	bannerSplitLines := strings.Split(strings.TrimRight(banner, "\n"), "\n")
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
	for i, line := range bannerSplitLines {
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

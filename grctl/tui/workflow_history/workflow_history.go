package workflow_history

import (
	"context"
	"fmt"
	"strings"
	"time"

	"grctl/server/grctl/tui/common"
	"grctl/server/grctl/tui/table"
	ext "grctl/server/types/external/v1"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type WorkflowHistoryModel struct {
	table            table.Model
	events           []*ext.HistoryEvent
	run              *ext.RunInfo
	err              error
	serverURL        string
	wfID             ext.WFID
	runID            ext.RunID
	header           common.Header
	loader           *HistoryLoader
	hasInput         bool
	hasOutput        bool
	hasError         bool
	detailOpen       bool
	detailFocused    bool
	detailViewport   viewport.Model
	confirmingCancel bool
	cancelErr        error
	width            int
	height           int
}

type historyLoadedMsg struct {
	events    []*ext.HistoryEvent
	loader    *HistoryLoader
	run       ext.RunInfo
	runID     ext.RunID
	hasInput  bool
	hasOutput bool
	hasError  bool
	err       error
}

type connectionStatusMsg struct {
	connected bool
}

type BackToListMsg struct{}

type cancelResultMsg struct {
	err error
}

const (
	headerTableGap   = 1
	titleTableGap    = 1
	runInfoBarHeight = 4
)

func NewWorkflowHistoryModel(serverURL string, wfID ext.WFID, runID ext.RunID, width, height int) WorkflowHistoryModel {
	return WorkflowHistoryModel{
		serverURL:      serverURL,
		wfID:           wfID,
		runID:          runID,
		header:         common.NewHeader("grctl", serverURL, false),
		width:          width,
		height:         height,
		detailViewport: viewport.New(0, 0),
	}
}

func (m WorkflowHistoryModel) Init() tea.Cmd {
	return loadHistory(m.serverURL, m.wfID, m.runID)
}

func (m WorkflowHistoryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = max(msg.Height-headerTableGap-titleTableGap-16, 1) // header(3) + run info(4) + help(2) + newlines(3)
		tableWidth := m.tableWidth()
		m.table.SetWidth(tableWidth - tablePadding*2)
		m.table.SetHeight(m.height)
		m.refreshDetailViewport(false)
		return m, nil

	case historyLoadedMsg:
		if msg.err != nil {
			if m.events == nil {
				m.err = msg.err
			}
			return m, nil
		}
		var cmd tea.Cmd
		if msg.loader != nil {
			m.loader = msg.loader
			cmd = watchConnection(m.loader.StatusCh())
		}
		m.err = nil
		m.run = &msg.run
		m.runID = msg.runID
		m.events = msg.events
		m.hasInput = msg.hasInput
		m.hasOutput = msg.hasOutput
		m.hasError = msg.hasError
		m.header.Connected = true
		if len(m.events) == 0 {
			m.detailOpen = false
			m.detailFocused = false
		}
		m.rebuildTable()
		if m.detailOpen {
			m.refreshDetailViewport(false)
		}
		return m, cmd

	case connectionStatusMsg:
		m.header.Connected = msg.connected
		return m, watchConnection(m.loader.StatusCh())

	case cancelResultMsg:
		m.cancelErr = msg.err
		if msg.err == nil {
			if m.loader != nil {
				return m, refreshHistory(m.loader, m.runID)
			}
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			if m.loader != nil {
				m.loader.Close()
			}
			return m, tea.Quit
		case "esc":
			if m.confirmingCancel {
				m.confirmingCancel = false
				return m, nil
			}
			if m.detailFocused {
				m.detailFocused = false
				return m, nil
			}
			if m.detailOpen {
				m.detailOpen = false
				m.rebuildTable()
				return m, nil
			}
			if m.loader != nil {
				m.loader.Close()
			}
			return m, func() tea.Msg { return BackToListMsg{} }
		case "r":
			if m.loader == nil {
				return m, loadHistory(m.serverURL, m.wfID, m.runID)
			}
			return m, refreshHistory(m.loader, m.runID)
		case "x":
			if m.run != nil && !m.run.Status.IsTerminal() && m.loader != nil {
				if m.confirmingCancel {
					m.confirmingCancel = false
					return m, cancelWorkflow(m.loader, m.wfID)
				}
				m.confirmingCancel = true
			}
			return m, nil
		case "enter":
			if len(m.events) > 0 && !m.detailOpen && m.width >= minPanelWidth {
				m.detailOpen = true
				m.rebuildTable()
				m.refreshDetailViewport(true)
			}
			return m, nil
		case "tab":
			if m.detailOpen {
				m.detailFocused = !m.detailFocused
				return m, nil
			}
		case "up":
			if m.detailFocused {
				m.detailViewport.ScrollUp(1)
				return m, nil
			}
		case "down":
			if m.detailFocused {
				m.detailViewport.ScrollDown(1)
				return m, nil
			}
		}
	}

	prevCursor := m.table.Cursor()
	m.table, cmd = m.table.Update(msg)
	if m.detailOpen && m.table.Cursor() != prevCursor {
		m.refreshDetailViewport(true)
	}
	return m, cmd
}

func (m WorkflowHistoryModel) View() string {
	var b strings.Builder

	b.WriteString(m.header.Render(m.width))
	b.WriteString(strings.Repeat("\n", headerTableGap))

	if m.err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("q quit • b back • r retry"))
		return b.String()
	}

	if m.events == nil {
		b.WriteString(loadingStyle.Render("Loading history..."))
		return b.String()
	}

	b.WriteString(renderRunInfo(m.run, m.runID, m.width, m.hasInput, m.hasOutput, m.hasError))
	b.WriteString(strings.Repeat("\n", titleTableGap))

	if len(m.events) == 0 {
		b.WriteString(emptyStateStyle.Render("No history events found."))
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("q quit • b back • r refresh"))
		return b.String()
	}

	cancellable := m.run != nil && !m.run.Status.IsTerminal()

	if m.detailOpen {
		tableView := tableContainerStyle.Render(m.table.View())
		leftWidth := m.tableWidth()
		rightWidth := max(m.width-leftWidth-1, 10) // 1 for border
		detailView := detailPanelStyle.Width(rightWidth).Render(m.detailViewport.View())

		borderColor := common.ColorTableBorder
		if m.detailFocused {
			borderColor = common.ColorMuted
		}
		border := strings.Repeat("│\n", max(m.height-1, 0)) + "│"
		borderView := lipgloss.NewStyle().Foreground(borderColor).Render(border)

		split := lipgloss.JoinHorizontal(lipgloss.Top, tableView, borderView, detailView)
		b.WriteString(split)
		b.WriteString("\n")
	} else {
		b.WriteString(tableContainerStyle.Render(m.table.View()))
		b.WriteString("\n")
	}

	if m.cancelErr != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("Cancel failed: %v", m.cancelErr)))
		b.WriteString("\n")
	}

	b.WriteString(renderHelpBar(m.detailOpen, m.detailFocused, cancellable, m.confirmingCancel))
	return b.String()
}

// refreshDetailViewport recomputes dimensions and content for the detail
// viewport from current model state. Must be called inside Update so changes
// are captured in the returned model. Pass resetScroll=true when the selected
// event changes so the new content is viewed from the top.
func (m *WorkflowHistoryModel) refreshDetailViewport(resetScroll bool) {
	leftWidth := m.tableWidth()
	rightWidth := max(m.width-leftWidth-1, 10)
	innerWidth := detailInnerWidth(rightWidth)
	m.detailViewport.Width = innerWidth
	m.detailViewport.Height = max(m.height-2, 1)

	cursor := m.table.Cursor()
	var event *ext.HistoryEvent
	if cursor >= 0 && cursor < len(m.events) {
		event = m.events[cursor]
	}
	m.detailViewport.SetContent(buildDetailContent(event, innerWidth))
	if resetScroll {
		m.detailViewport.GotoTop()
	}
}

func (m *WorkflowHistoryModel) rebuildTable() {
	cursor := m.table.Cursor()
	m.table = m.createTable()
	m.table.SetCursor(cursor)
}

func (m WorkflowHistoryModel) tableWidth() int {
	if m.detailOpen {
		return condensedTableWidth
	}
	return m.width
}

func (m WorkflowHistoryModel) createTable() table.Model {
	var columns []table.Column
	rows := make([]table.Row, 0, len(m.events))

	if m.detailOpen {
		columns = []table.Column{
			{Title: "TIME", Width: 12},
			{Title: "KIND", Width: 18},
			{Title: "DURATION", Width: 10},
		}
		for _, event := range m.events {
			rows = append(rows, table.Row{
				formatTimeShort(event.Timestamp),
				formatKindLabel(event.Kind),
				extractDuration(event),
			})
		}
	} else {
		columns = []table.Column{
			{Title: "TIME", Width: 25},
			{Title: "KIND", Width: 18},
			{Title: "DETAILS", Flex: 11},
			{Title: "DURATION", Width: 10},
		}
		for _, event := range m.events {
			rows = append(rows, table.Row{
				formatTime(event.Timestamp),
				formatKindLabel(event.Kind),
				eventDetails(event),
				extractDuration(event),
			})
		}
	}

	styles := getTableStyles()
	tableWidth := m.tableWidth() - tablePadding*2

	kindCol := kindColumnIndex
	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithWidth(tableWidth),
		table.WithHeight(m.height),
		table.WithStyles(styles),
		table.WithStyleFunc(func(row, col int, _ string) lipgloss.Style {
			if col == kindCol && row < len(m.events) {
				return kindCellStyle(styles.Cell, m.events[row].Kind)
			}
			return styles.Cell
		}),
	)
	return t
}

func formatTimeShort(t time.Time) string {
	return t.Format("15:04:05.000")
}

func renderRunInfo(run *ext.RunInfo, runID ext.RunID, width int, hasInput, hasOutput, hasError bool) string {
	const leftLabelWidth = 12
	const rightLabelWidth = 10

	wfType := "-"
	wfID := "-"
	statusLabel := "-"
	var status ext.RunStatus
	hasStatus := false
	startedAt := "-"
	completedAt := "-"
	duration := "-"
	if run != nil {
		wfType = string(run.WFType)
		wfID = run.WFID.String()
		status = run.Status
		hasStatus = true
		statusLabel = formatStatusLabel(run.Status)
		startedAt = formatOptionalTime(run.StartedAt)
		completedAt = formatOptionalTime(run.CompletedAt)
		duration = formatOptionalDuration(run.StartedAt, run.CompletedAt)
	}

	leftText := lipgloss.JoinVertical(
		lipgloss.Left,
		runInfoTitleStyle.Render(wfType),
		runInfoStatusStyle(status, hasStatus).Render(formatLabelValue("Status", leftLabelWidth, statusLabel)),
		runInfoMetaStyle.Render(formatLabelValue("Workflow ID", leftLabelWidth, wfID)),
		runInfoMetaStyle.Render(formatLabelValue("Run ID", leftLabelWidth, runID.String())),
	)
	rightText := lipgloss.JoinVertical(
		lipgloss.Left,
		"",
		runInfoMetaStyle.Render(formatLabelValue("Started", rightLabelWidth, startedAt)),
		runInfoMetaStyle.Render(formatLabelValue("Completed", rightLabelWidth, completedAt)),
		runInfoMetaStyle.Render(formatLabelValue("Duration", rightLabelWidth, duration)),
	)

	badgeText := renderRunBadges(hasInput, hasOutput, hasError)

	bar := titleBarStyle.Render(strings.Repeat("┃\n", runInfoBarHeight-1) + "┃")
	base := lipgloss.JoinHorizontal(lipgloss.Top, bar, leftText, runInfoSpacerStyle.Render(""), rightText)
	if badgeText == "" {
		return runInfoContainerStyle.Render(base)
	}

	innerWidth := max(width-runInfoHorizontalPadding, 0)
	baseWidth := lipgloss.Width(base)
	badgeWidth := lipgloss.Width(badgeText)
	spacerWidth := max(innerWidth-baseWidth-badgeWidth, 1)
	spacer := lipgloss.NewStyle().Width(spacerWidth).Render("")

	line := lipgloss.JoinHorizontal(lipgloss.Top, base, spacer, badgeText)
	return runInfoContainerStyle.Render(line)
}

func renderRunBadges(hasInput, hasOutput, hasError bool) string {
	if !hasInput && !hasOutput && !hasError {
		return ""
	}

	badges := []string{}
	if hasInput {
		badges = append(badges, runInfoBadgeInputStyle.Render("IN"))
	}
	if hasOutput {
		badges = append(badges, runInfoBadgeOutputStyle.Render("OUT"))
	}
	if hasError {
		badges = append(badges, runInfoBadgeErrorStyle.Render("ERR"))
	}

	return strings.Join(badges, " ")
}

func formatStatusLabel(status ext.RunStatus) string {
	return strings.ToUpper(strings.ReplaceAll(string(status), "_", " "))
}

func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return formatTime(*t)
}

func formatOptionalDuration(start, end *time.Time) string {
	if start == nil || end == nil {
		return "-"
	}
	return formatDuration(end.Sub(*start))
}

func formatLabelValue(label string, width int, value string) string {
	return fmt.Sprintf("%-*s %s", width, label+":", value)
}

func loadHistory(serverURL string, wfID ext.WFID, runID ext.RunID) tea.Cmd {
	return func() tea.Msg {
		loader, err := NewHistoryLoader(serverURL)
		if err != nil {
			return historyLoadedMsg{err: err}
		}

		var run ext.RunInfo
		if runID != "" {
			run, err = loader.GetRunInfoByRunID(context.Background(), runID)
		} else {
			run, err = loader.GetLatestRunInfo(context.Background(), wfID)
		}
		if err != nil {
			loader.Close()
			return historyLoadedMsg{err: err}
		}
		runID = run.ID

		events, err := loader.GetHistory(context.Background(), run.WFID, runID)
		if err != nil {
			loader.Close()
			return historyLoadedMsg{err: err}
		}

		hasInput, hasOutput, hasError, err := loader.GetRunDataFlags(context.Background(), run.WFID, runID)
		if err != nil {
			loader.Close()
			return historyLoadedMsg{err: err}
		}

		return historyLoadedMsg{events: events, loader: loader, run: run, runID: runID, hasInput: hasInput, hasOutput: hasOutput, hasError: hasError}
	}
}

func refreshHistory(loader *HistoryLoader, runID ext.RunID) tea.Cmd {
	return func() tea.Msg {
		run, err := loader.GetRunInfoByRunID(context.Background(), runID)
		if err != nil {
			return historyLoadedMsg{err: err}
		}

		events, err := loader.GetHistory(context.Background(), run.WFID, runID)
		if err != nil {
			return historyLoadedMsg{err: err}
		}

		hasInput, hasOutput, hasError, err := loader.GetRunDataFlags(context.Background(), run.WFID, runID)
		if err != nil {
			return historyLoadedMsg{err: err}
		}

		return historyLoadedMsg{events: events, run: run, runID: runID, hasInput: hasInput, hasOutput: hasOutput, hasError: hasError}
	}
}

// watchConnection is a Bubble Tea subscription that blocks until the next
// NATS connection status change, then delivers it as a connectionStatusMsg.
// The caller must re-issue this cmd after each message to continue watching.
func watchConnection(statusCh <-chan bool) tea.Cmd {
	return func() tea.Msg {
		connected, ok := <-statusCh
		if !ok {
			return nil
		}
		return connectionStatusMsg{connected: connected}
	}
}

// extractDuration returns a formatted duration string for events that carry one.
func extractDuration(event *ext.HistoryEvent) string {
	var ms int64
	switch msg := event.Msg.(type) {
	case *ext.RunCompleted:
		ms = msg.DurationMS
	case *ext.RunFailed:
		ms = msg.DurationMS
	case *ext.RunCancelled:
		ms = msg.DurationMS
	case *ext.RunTimeout:
		ms = msg.DurationMS
	case *ext.StepCompleted:
		ms = msg.DurationMS
	case *ext.StepFailed:
		ms = msg.DurationMS
	case *ext.StepTimedout:
		ms = msg.DurationMS
	case *ext.TaskCompleted:
		ms = msg.DurationMS
	case *ext.TaskFailed:
		ms = msg.DurationMS
	case *ext.TaskCancelled:
		ms = msg.DurationMS
	default:
		return "-"
	}
	return formatDuration(time.Duration(ms) * time.Millisecond)
}

func formatTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05.000")
}

func cancelWorkflow(loader *HistoryLoader, wfID ext.WFID) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return cancelResultMsg{err: loader.CancelWorkflow(ctx, wfID)}
	}
}

func renderHelpBar(detailOpen, detailFocused, cancellable, confirmingCancel bool) string {
	if confirmingCancel {
		return helpStyle.Render("x confirm cancel • esc abort")
	}
	cancelHint := ""
	if cancellable {
		cancelHint = " • x cancel"
	}
	if detailOpen && detailFocused {
		return helpStyle.Render("q quit • ↑/↓ scroll • tab table • esc unfocus • r refresh" + cancelHint)
	}
	if detailOpen {
		return helpStyle.Render("q quit • ↑/↓ navigate • tab focus detail • esc close • r refresh" + cancelHint)
	}
	return helpStyle.Render("q quit • ↑/↓ navigate • enter details • esc back • r refresh" + cancelHint)
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%.0fs", int(d.Minutes()), d.Seconds()-float64(int(d.Minutes()))*60)
}

package workflow_list

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"grctl/server/grctl/tui/common"
	"grctl/server/grctl/tui/table"
	"grctl/server/natsreg"
	"grctl/server/store"
	ext "grctl/server/types/external/v1"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type WorkflowListModel struct {
	table     table.Model
	runs      []*ext.RunInfo
	err       error
	quitting  bool
	serverURL string
	header    common.Header
	nc        *nats.Conn
	store     *store.StateStore
	statusCh  chan bool
	width     int
	height    int
}

type workflowsLoadedMsg struct {
	runs     []*ext.RunInfo
	nc       *nats.Conn
	store    *store.StateStore
	statusCh chan bool
	err      error
}

type OpenHistoryMsg struct {
	WFType ext.WFType
	WFID   ext.WFID
	RunID  ext.RunID
}

type connectionStatusMsg struct {
	connected bool
}

const (
	headerTableGap = 1
	titleTableGap  = 2
)

func NewWorkflowListModel(serverURL string) WorkflowListModel {
	return WorkflowListModel{
		serverURL: serverURL,
		header:    common.NewHeader("grctl", serverURL, false),
	}
}

func (m WorkflowListModel) Init() tea.Cmd {
	return loadWorkflows(m.serverURL)
}

func (m WorkflowListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = max(msg.Height-headerTableGap-titleTableGap-10, 1) // header(3) + title(3) + help(2) + newline(2)
		m.table.SetWidth(msg.Width - tablePadding*2)
		m.table.SetHeight(m.height)
		return m, nil

	case workflowsLoadedMsg:
		if msg.err != nil {
			if m.runs == nil {
				m.err = msg.err
			}
			// Refresh failed — keep existing data
			return m, nil
		}
		var cmd tea.Cmd
		if msg.nc != nil {
			m.nc = msg.nc
			m.store = msg.store
			m.statusCh = msg.statusCh
			cmd = watchConnection(m.statusCh)
		}
		m.err = nil
		m.runs = msg.runs
		m.header.Connected = true
		m.table = m.createTable()
		return m, cmd

	case connectionStatusMsg:
		m.header.Connected = msg.connected
		return m, watchConnection(m.statusCh)

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			if m.nc != nil {
				m.nc.Close()
			}
			return m, tea.Quit
		case "enter":
			if len(m.runs) == 0 {
				return m, nil
			}
			r := m.runs[m.table.Cursor()]
			return m, func() tea.Msg {
				return OpenHistoryMsg{WFType: r.WFType, WFID: r.WFID, RunID: r.ID}
			}
		case "r":
			if m.store == nil {
				return m, loadWorkflows(m.serverURL)
			}
			return m, refreshWorkflows(m.store)
		}
	}

	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m WorkflowListModel) View() string {
	var b strings.Builder

	// Render header
	b.WriteString(m.header.Render(m.width))
	b.WriteString(strings.Repeat("\n", headerTableGap))

	if m.err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("q quit • r retry"))
		return b.String()
	}

	if m.runs == nil {
		b.WriteString(loadingStyle.Render("Loading workflows..."))
		return b.String()
	}

	if len(m.runs) == 0 {
		b.WriteString(emptyStateStyle.Render("No workflow runs found."))
		return b.String()
	}

	b.WriteString(titleStyle.Render(titleBarStyle.Render("█") + " Workflow Runs"))
	b.WriteString(strings.Repeat("\n", titleTableGap))
	b.WriteString(tableContainerStyle.Render(m.table.View()))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("q quit • ↑/↓ navigate • enter history • r refresh"))
	return b.String()
}

func (m WorkflowListModel) createTable() table.Model {
	columns := []table.Column{
		{Title: "TYPE", Flex: 4},
		{Title: "STATUS", Width: 12},
		{Title: "WORKFLOW ID", Flex: 2},
		{Title: "RUN ID", Flex: 2},
		{Title: "CREATED", Flex: 2},
		{Title: "STARTED", Flex: 2},
		{Title: "COMPLETED", Flex: 2},
	}

	slices.SortFunc(m.runs, func(a, b *ext.RunInfo) int {
		if a.StartedAt == nil && b.StartedAt == nil {
			return 0
		}
		if a.StartedAt == nil {
			return 1
		}
		if b.StartedAt == nil {
			return -1
		}
		return b.StartedAt.Compare(*a.StartedAt)
	})

	rows := make([]table.Row, 0, len(m.runs))
	for _, run := range m.runs {
		rows = append(rows, table.Row{
			string(run.WFType),
			formatStatusLabel(run.Status),
			run.WFID.String(),
			run.ID.String(),
			formatTime(run.CreatedAt),
			formatOptionalTime(run.StartedAt),
			formatOptionalTime(run.CompletedAt),
		})
	}

	styles := getTableStyles()

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithWidth(m.width-tablePadding*2),
		table.WithHeight(m.height),
		table.WithStyles(styles),
		table.WithStyleFunc(func(row, col int, _ string) lipgloss.Style {
			if col == statusColumnIndex && row < len(m.runs) {
				return statusCellStyle(styles.Cell, m.runs[row].Status).Align(lipgloss.Center)
			}
			return styles.Cell
		}),
	)
	return t
}

func loadWorkflows(serverURL string) tea.Cmd {
	return func() tea.Msg {
		statusCh := make(chan bool, 2)
		nc, err := nats.Connect(serverURL,
			nats.DisconnectErrHandler(func(_ *nats.Conn, _ error) {
				select {
				case statusCh <- false:
				default:
				}
			}),
			nats.ReconnectHandler(func(_ *nats.Conn) {
				select {
				case statusCh <- true:
				default:
				}
			}),
		)
		if err != nil {
			return workflowsLoadedMsg{err: fmt.Errorf("failed to connect to server at %s: %w", serverURL, err)}
		}

		js, err := jetstream.New(nc)
		if err != nil {
			nc.Close()
			return workflowsLoadedMsg{err: fmt.Errorf("failed to create JetStream context: %w", err)}
		}

		stream, err := js.Stream(context.Background(), natsreg.Manifest.StateStreamName())
		if err != nil {
			nc.Close()
			return workflowsLoadedMsg{err: fmt.Errorf("failed to bind to state stream: %w", err)}
		}

		runStore := store.NewStateStore(js, stream)
		runs, err := runStore.ListRuns(context.Background())
		if err != nil {
			nc.Close()
			return workflowsLoadedMsg{err: err}
		}

		return workflowsLoadedMsg{runs: runs, nc: nc, store: runStore, statusCh: statusCh}
	}
}

func refreshWorkflows(runStore *store.StateStore) tea.Cmd {
	return func() tea.Msg {
		runs, err := runStore.ListRuns(context.Background())
		if err != nil {
			return workflowsLoadedMsg{err: err}
		}
		return workflowsLoadedMsg{runs: runs}
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

func formatTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return formatTime(*t)
}

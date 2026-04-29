package workflow_browser

import (
	"grctl/server/grctl/tui/workflow_history"
	"grctl/server/grctl/tui/workflow_list"

	tea "github.com/charmbracelet/bubbletea"
)

type screen int

const (
	screenList screen = iota
	screenHistory
)

type WorkflowBrowserModel struct {
	serverURL  string
	list       workflow_list.WorkflowListModel
	history    workflow_history.WorkflowHistoryModel
	hasHistory bool
	active     screen
	width      int
	height     int
}

func NewWorkflowBrowserModel(serverURL string) WorkflowBrowserModel {
	list := workflow_list.NewWorkflowListModel(serverURL)
	return WorkflowBrowserModel{
		serverURL: serverURL,
		list:      list,
		active:    screenList,
	}
}

func (m WorkflowBrowserModel) Init() tea.Cmd {
	return m.list.Init()
}

func (m WorkflowBrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case workflow_list.OpenHistoryMsg:
		history := workflow_history.NewWorkflowHistoryModel(m.serverURL, msg.WFID, msg.RunID, 0, 0)
		m.history = history
		m.hasHistory = true
		m.active = screenHistory
		if m.width > 0 && m.height > 0 {
			return m, tea.Batch(
				history.Init(),
				func() tea.Msg { return tea.WindowSizeMsg{Width: m.width, Height: m.height} },
			)
		}
		return m, history.Init()
	case workflow_history.BackToListMsg:
		m.active = screenList
		if m.width > 0 && m.height > 0 {
			return m, func() tea.Msg { return tea.WindowSizeMsg{Width: m.width, Height: m.height} }
		}
		return m, nil
	}

	switch m.active {
	case screenList:
		updated, cmd := m.list.Update(msg)
		if model, ok := updated.(workflow_list.WorkflowListModel); ok {
			m.list = model
		}
		return m, cmd
	case screenHistory:
		if !m.hasHistory {
			return m, nil
		}
		updated, cmd := m.history.Update(msg)
		if model, ok := updated.(workflow_history.WorkflowHistoryModel); ok {
			m.history = model
		}
		return m, cmd
	default:
		return m, nil
	}
}

func (m WorkflowBrowserModel) View() string {
	if m.active == screenHistory && m.hasHistory {
		return m.history.View()
	}
	return m.list.View()
}

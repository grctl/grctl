package workflow_list

import (
	"strings"

	"grctl/server/grctl/tui/common"
	"grctl/server/grctl/tui/table"
	"grctl/server/types/external/v1"

	"github.com/charmbracelet/lipgloss"
)

var (
	titleBarStyle = lipgloss.NewStyle().
			Foreground(common.ColorStatusRunning)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(common.ColorPrimary).
			Padding(2, 1, 1, 2)

	errorStyle = lipgloss.NewStyle().
			Foreground(common.ColorStatusError).
			Padding(0, common.PaddingSmall)

	emptyStateStyle = lipgloss.NewStyle().
			Foreground(common.ColorMuted).
			Italic(true).
			Padding(common.PaddingSmall, common.PaddingMedium)

	loadingStyle = lipgloss.NewStyle().
			Foreground(common.ColorInfo).
			Padding(common.PaddingSmall, common.PaddingMedium)

	helpStyle = lipgloss.NewStyle().
			Foreground(common.ColorMuted).
			Padding(2, 0, 0, 2)
)

const (
	statusColumnIndex = 1
	tablePadding      = 2
)

var tableContainerStyle = lipgloss.NewStyle().Padding(0, tablePadding)

func getTableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(common.ColorTableBorder).
		BorderBottom(true).
		Foreground(common.ColorPrimary).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(common.ColorHighlight).
		Background(common.ColorAccent).
		Bold(true)

	return s
}

func formatStatusLabel(status external.RunStatus) string {
	return strings.ToUpper(strings.ReplaceAll(string(status), "_", " "))
}

func statusCellStyle(base lipgloss.Style, status external.RunStatus) lipgloss.Style {
	style := base.Bold(true)

	switch status {
	case external.RunStatusRunning:
		return style.Foreground(common.ColorStatusRunning)
	case external.RunStatusCompleted:
		return style.Foreground(common.ColorStatusSuccess)
	case external.RunStatusFailed:
		return style.Foreground(common.ColorStatusError)
	case external.RunStatusCancelled, external.RunStatusTimedOut:
		return style.Foreground(common.ColorStatusWarning)
	default:
		return style.Foreground(common.ColorStatusNeutral)
	}
}

package workflow_history

import (
	"strings"

	"grctl/server/grctl/tui/common"
	"grctl/server/grctl/tui/table"
	ext "grctl/server/types/external/v1"

	"github.com/charmbracelet/lipgloss"
)

var (
	titleBarStyle = lipgloss.NewStyle().
			Foreground(common.ColorStatusRunning)

	runInfoTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(common.ColorPrimary).
				Padding(0, 0, 0, 2)

	runInfoMetaStyle = lipgloss.NewStyle().
				Foreground(common.ColorMuted).
				Padding(0, 0, 0, 2)

	runInfoBadgeStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#ffffff")).
				Padding(0, 1)

	runInfoBadgeInputStyle  = runInfoBadgeStyle.Background(lipgloss.Color("#1d4ed8")) // Blue
	runInfoBadgeOutputStyle = runInfoBadgeStyle.Background(lipgloss.Color("#15803d")) // Green
	runInfoBadgeErrorStyle  = runInfoBadgeStyle.Background(lipgloss.Color("#b91c1c")) // Red

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
	kindColumnIndex          = 1
	tablePadding             = 2
	runInfoHorizontalPadding = 4
	condensedTableWidth      = 48
	minPanelWidth            = 80
)

var tableContainerStyle = lipgloss.NewStyle().Padding(0, tablePadding)

var runInfoContainerStyle = lipgloss.NewStyle().Padding(2, 2)

var runInfoSpacerStyle = lipgloss.NewStyle().Width(4)

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

func formatKindLabel(kind ext.HistoryKind) string {
	return strings.ToUpper(strings.ReplaceAll(string(kind), ".", " "))
}

func kindColor(kind ext.HistoryKind) lipgloss.Color {
	switch {
	case strings.HasPrefix(string(kind), "run."):
		return common.ColorCategoryRun
	case strings.HasPrefix(string(kind), "step."):
		return common.ColorCategoryStep
	case strings.HasPrefix(string(kind), "task."):
		return common.ColorCategoryTask
	case strings.HasPrefix(string(kind), "wait_event."), strings.HasPrefix(string(kind), "event."):
		return common.ColorCategoryEvent
	case strings.HasPrefix(string(kind), "child."), strings.HasPrefix(string(kind), "parent."):
		return common.ColorCategoryChild
	default:
		return common.ColorStatusNeutral
	}
}

func kindCellStyle(base lipgloss.Style, kind ext.HistoryKind) lipgloss.Style {
	return base.Bold(true).Foreground(kindColor(kind))
}

func runInfoStatusStyle(status ext.RunStatus, hasStatus bool) lipgloss.Style {
	style := runInfoMetaStyle.Bold(true)
	if !hasStatus {
		return style.Foreground(common.ColorStatusNeutral)
	}

	switch status {
	case ext.RunStatusRunning:
		return style.Foreground(common.ColorStatusRunning)
	case ext.RunStatusCompleted:
		return style.Foreground(common.ColorStatusSuccess)
	case ext.RunStatusFailed:
		return style.Foreground(common.ColorStatusError)
	case ext.RunStatusCancelled, ext.RunStatusTimedOut:
		return style.Foreground(common.ColorStatusWarning)
	default:
		return style.Foreground(common.ColorStatusNeutral)
	}
}

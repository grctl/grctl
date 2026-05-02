package workflow_history

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	chromaFormatters "github.com/alecthomas/chroma/v2/formatters"
	chromaLexers "github.com/alecthomas/chroma/v2/lexers"
	chromaStyles "github.com/alecthomas/chroma/v2/styles"

	"grctl/server/grctl/tui/common"
	ext "grctl/server/types/external/v1"

	"github.com/charmbracelet/lipgloss"
)

var (
	chromaJSONLexer = chromaLexers.Get("json")
	chromaFormatter = chromaFormatters.Get("terminal256")
	chromaStyle     = chromaStyles.Get("dracula")
)

var (
	detailTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Padding(0, 1)

	detailTimestampStyle = lipgloss.NewStyle().
				Foreground(common.ColorMuted)

	detailLabelStyle = lipgloss.NewStyle().
				Foreground(common.ColorMuted)

	detailValueStyle = lipgloss.NewStyle().
				Foreground(common.ColorPrimary)

	detailSectionStyle = lipgloss.NewStyle().
				Foreground(common.ColorMuted)

	detailContentStyle = lipgloss.NewStyle().
				Foreground(common.ColorPrimary)

	detailPanelStyle = lipgloss.NewStyle().
				Padding(1, 2)
)

const detailLabelWidth = 13

// detailInnerWidth returns the content width for the detail panel, accounting
// for panel padding and the vertical border separating it from the table.
func detailInnerWidth(panelWidth int) int {
	return max(panelWidth-6, 10)
}

// buildDetailContent returns the full unstyled content string for a history
// event. The caller (viewport) handles height clipping and scrolling.
func buildDetailContent(event *ext.HistoryEvent, width int) string {
	if event == nil {
		return ""
	}
	return strings.Join(buildDetailLines(event, width), "\n")
}

func buildDetailLines(event *ext.HistoryEvent, width int) []string {
	var lines []string

	// Line 1: Kind label (colored) + timestamp right-aligned
	kindLabel := formatKindLabel(event.Kind)
	kindStyle := detailTitleStyle.Foreground(kindColor(event.Kind))
	timestamp := event.Timestamp.Format("15:04:05.000")

	kindRendered := kindStyle.Render(kindLabel)
	kindWidth := lipgloss.Width(kindRendered)
	tsRendered := detailTimestampStyle.Render(timestamp)
	tsWidth := lipgloss.Width(tsRendered)
	gap := max(width-kindWidth-tsWidth, 1)
	lines = append(lines, kindRendered+strings.Repeat(" ", gap)+tsRendered)

	// Blank line
	lines = append(lines, "")

	// Key-value fields
	kvLines := buildKeyValueLines(event)
	lines = append(lines, kvLines...)

	// Data sections
	dataLines := buildDataSections(event, width)
	if len(dataLines) > 0 {
		lines = append(lines, "")
		lines = append(lines, dataLines...)
	}

	return lines
}

func buildKeyValueLines(event *ext.HistoryEvent) []string {
	var lines []string

	switch msg := event.Msg.(type) {
	case *ext.RunScheduled:
		lines = appendKV(lines, "Run ID", event.RunID.String())
	case *ext.RunStarted:
		lines = appendKV(lines, "Run ID", event.RunID.String())
	case *ext.RunCompleted:
		lines = appendKV(lines, "Run ID", event.RunID.String())
		lines = appendKV(lines, "Duration", formatDuration(time.Duration(msg.DurationMS)*time.Millisecond))
	case *ext.RunFailed:
		lines = appendKV(lines, "Duration", formatDuration(time.Duration(msg.DurationMS)*time.Millisecond))
	case *ext.RunCancelScheduled:
		lines = appendKV(lines, "Run ID", event.RunID.String())
	case *ext.RunCancelReceived:
		lines = appendKV(lines, "Run ID", event.RunID.String())
	case *ext.RunCancelled:
		lines = appendKV(lines, "Run ID", event.RunID.String())
		lines = appendKV(lines, "Reason", msg.Reason)
		lines = appendKV(lines, "Duration", formatDuration(time.Duration(msg.DurationMS)*time.Millisecond))
	case *ext.RunTimeout:
		lines = appendKV(lines, "Run ID", event.RunID.String())
		lines = appendKV(lines, "Duration", formatDuration(time.Duration(msg.DurationMS)*time.Millisecond))

	case *ext.StepStarted:
		lines = appendKV(lines, "Step", msg.StepName)
	case *ext.StepCompleted:
		lines = appendKV(lines, "Step", msg.StepName)
		lines = appendKV(lines, "Worker", string(msg.WorkerID))
		lines = appendKV(lines, "Duration", formatDuration(time.Duration(msg.DurationMS)*time.Millisecond))
	case *ext.StepFailed:
		lines = appendKV(lines, "Step", msg.StepName)
		lines = appendKV(lines, "Duration", formatDuration(time.Duration(msg.DurationMS)*time.Millisecond))
	case *ext.StepCancelled:
		lines = appendKV(lines, "Step", msg.StepName)
	case *ext.StepTimedout:
		lines = appendKV(lines, "Step", msg.StepName)
		lines = appendKV(lines, "Duration", formatDuration(time.Duration(msg.DurationMS)*time.Millisecond))

	case *ext.TaskStarted:
		lines = appendKV(lines, "Task", msg.TaskName)
		lines = appendKV(lines, "Task ID", msg.TaskID)
		lines = appendKV(lines, "Step", msg.StepName)
	case *ext.TaskCompleted:
		lines = appendKV(lines, "Task", msg.TaskName)
		lines = appendKV(lines, "Task ID", msg.TaskID)
		lines = appendKV(lines, "Step", msg.StepName)
		lines = appendKV(lines, "Duration", formatDuration(time.Duration(msg.DurationMS)*time.Millisecond))
	case *ext.TaskFailed:
		lines = appendKV(lines, "Task", msg.TaskName)
		lines = appendKV(lines, "Task ID", msg.TaskID)
		lines = appendKV(lines, "Step", msg.StepName)
		lines = appendKV(lines, "Duration", formatDuration(time.Duration(msg.DurationMS)*time.Millisecond))
	case *ext.TaskCancelled:
		lines = appendKV(lines, "Task", msg.TaskName)
		lines = appendKV(lines, "Task ID", msg.TaskID)
		lines = appendKV(lines, "Step", msg.StepName)
		lines = appendKV(lines, "Duration", formatDuration(time.Duration(msg.DurationMS)*time.Millisecond))

	case *ext.EventReceived:
		lines = appendKV(lines, "Event Name", msg.EventName)

	case *ext.ParentEventSent:
		lines = appendKV(lines, "Event Name", msg.EventName)
		if msg.ParentWFType != "" {
			lines = appendKV(lines, "Parent", msg.ParentWFType)
		}
		if msg.ParentWFID != "" {
			lines = appendKV(lines, "Parent WF ID", msg.ParentWFID)
		}

	case *ext.ChildWorkflowStarted:
		lines = appendKV(lines, "Workflow", msg.WFType)
		lines = appendKV(lines, "Workflow ID", msg.WFID.String())
		lines = appendKV(lines, "Run ID", msg.RunID.String())

	case nil:
		// No fields for nil message
	}

	return lines
}

func buildDataSections(event *ext.HistoryEvent, width int) []string {
	var lines []string

	switch msg := event.Msg.(type) {
	case *ext.RunStarted:
		lines = appendDataSection(lines, "Input", msg.Input, width)
	case *ext.RunCompleted:
		lines = appendDataSection(lines, "Result", msg.Result, width)
	case *ext.RunFailed:
		lines = appendErrorSection(lines, msg.Error, width)
	case *ext.StepFailed:
		lines = appendErrorSection(lines, msg.Error, width)
	case *ext.TaskStarted:
		lines = appendDataSection(lines, "Input", msg.Args, width)
	case *ext.TaskCompleted:
		lines = appendDataSection(lines, "Output", msg.Output, width)
	case *ext.TaskFailed:
		lines = appendErrorSection(lines, msg.Error, width)
	case *ext.EventReceived:
		lines = appendDataSection(lines, "Payload", msg.Payload, width)
	case *ext.ParentEventSent:
		lines = appendDataSection(lines, "Payload", msg.Payload, width)
	case *ext.ChildWorkflowStarted:
		lines = appendDataSection(lines, "Input", msg.Input, width)
	}

	return lines
}

func appendKV(lines []string, label, value string) []string {
	rendered := fmt.Sprintf(" %s %s",
		detailLabelStyle.Render(fmt.Sprintf("%-*s", detailLabelWidth, label+":")),
		detailValueStyle.Render(value),
	)
	return append(lines, rendered)
}

func appendDataSection(lines []string, label string, data any, width int) []string {
	lines = append(lines, renderSectionRule(label, width))
	lines = append(lines, prettyPrintJSON(data)...)
	return lines
}

func appendErrorSection(lines []string, err ext.ErrorDetails, width int) []string {
	lines = append(lines, renderSectionRule("Error", width))
	lines = append(lines, detailValueStyle.Render(" "+err.Type))
	lines = append(lines, detailContentStyle.Render(" "+err.Message))
	if err.StackTrace != "" {
		lines = append(lines, "")
		lines = append(lines, renderSectionRule("Stack Trace", width))
		for _, traceLine := range strings.Split(strings.TrimRight(err.StackTrace, "\n"), "\n") {
			lines = append(lines, detailContentStyle.Render(" "+traceLine))
		}
	}
	return lines
}

func renderSectionRule(label string, width int) string {
	prefix := " ── " + label + " "
	remaining := max(width-lipgloss.Width(prefix), 3)
	return detailSectionStyle.Render(prefix + strings.Repeat("─", remaining))
}

func prettyPrintJSON(v any) []string {
	if v == nil {
		return []string{detailContentStyle.Render(" null")}
	}
	b, err := json.MarshalIndent(v, " ", "  ")
	if err != nil {
		return []string{detailContentStyle.Render(" " + fmt.Sprintf("%v", v))}
	}
	raw := string(b)

	if lines := highlightJSON(raw); lines != nil {
		return lines
	}

	// Fallback: uniform lipgloss color (original behavior).
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		lines = append(lines, detailContentStyle.Render(" "+line))
	}
	return lines
}

func highlightJSON(raw string) []string {
	if chromaJSONLexer == nil || chromaFormatter == nil || chromaStyle == nil {
		return nil
	}
	iterator, err := chromaJSONLexer.Tokenise(nil, raw)
	if err != nil {
		return nil
	}
	var buf bytes.Buffer
	if err := chromaFormatter.Format(&buf, chromaStyle, iterator); err != nil {
		return nil
	}
	// Chroma appends a trailing newline via EnsureNL — trim to avoid a phantom empty line.
	highlighted := strings.TrimRight(buf.String(), "\n")
	rawLines := strings.Split(highlighted, "\n")
	out := make([]string, len(rawLines))
	for i, line := range rawLines {
		// No lipgloss wrapping: Chroma's ANSI codes and lipgloss resets conflict.
		out[i] = " " + line
	}
	return out
}

package common

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	headerContainerStyle = lipgloss.NewStyle().
				Background(ColorHeaderBg).
				PaddingTop(PaddingSmall).
				PaddingBottom(PaddingSmall).
				PaddingLeft(PaddingMedium).
				PaddingRight(PaddingMedium)

	appNameStyle = lipgloss.NewStyle().
			Bold(true).
			Background(ColorHeaderBg).
			Foreground(ColorPrimary)

	serverInfoStyle = lipgloss.NewStyle().
			Background(ColorHeaderBg)

	statusDotStyle = lipgloss.NewStyle().
			Background(ColorHeaderBg).
			Bold(true)

	spacerStyle = lipgloss.NewStyle().
			Background(ColorHeaderBg)
)

type Header struct {
	AppName   string
	NatsURL   string
	Connected bool
}

func NewHeader(appName string, natsURL string, connected bool) Header {
	return Header{
		AppName:   appName,
		NatsURL:   natsURL,
		Connected: connected,
	}
}

func (h Header) Render(width int) string {
	if width == 0 {
		width = 80
	}

	leftSide := appNameStyle.Render(h.AppName)
	rightSide := h.renderConnectionStatus()
	contentWidth := max(width-(PaddingMedium*2), 1)

	spacerWidth := max(contentWidth-lipgloss.Width(leftSide)-lipgloss.Width(rightSide), 1)
	spacer := spacerStyle.Render(strings.Repeat(" ", spacerWidth))

	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		leftSide,
		spacer,
		rightSide,
	)

	header := headerContainerStyle.
		Width(width).
		Render(content)

	return header
}

func (h Header) renderConnectionStatus() string {
	var statusIcon string
	var statusColor lipgloss.Color

	if h.Connected {
		statusIcon = "●"
		statusColor = ColorStatusSuccess
	} else {
		statusIcon = "○"
		statusColor = ColorStatusError
	}

	status := statusDotStyle.
		Foreground(statusColor).
		Render(statusIcon)

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		status,
		serverInfoStyle.Render(" "+h.NatsURL),
	)
}

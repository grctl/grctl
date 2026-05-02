package common

import "github.com/charmbracelet/lipgloss"

// Color palette
var (
	ColorPrimary   = lipgloss.Color("255") // Classic gray
	ColorWarning   = lipgloss.Color("246") // Medium gray
	ColorMuted     = lipgloss.Color("244") // Muted gray
	ColorInfo      = lipgloss.Color("248") // Soft gray
	ColorHighlight = lipgloss.Color("255") // Near white
	ColorAccent    = lipgloss.Color("235") // Muted gray
	ColorHeaderBg  = lipgloss.Color("233") // Dark gray

	ColorStatusRunning = lipgloss.Color("12")  // Bright Blue
	ColorStatusSuccess = lipgloss.Color("10")  // Bright Green
	ColorStatusError   = lipgloss.Color("9")   // Bright Red
	ColorStatusWarning = lipgloss.Color("11")  // Bright Yellow
	ColorStatusNeutral = lipgloss.Color("244") // Muted gray

	// History event category colors
	ColorCategoryRun   = lipgloss.Color("12") // Bright Blue
	ColorCategoryStep  = lipgloss.Color("14") // Bright Cyan
	ColorCategoryTask  = lipgloss.Color("13") // Bright Magenta
	ColorCategoryEvent = lipgloss.Color("11") // Bright Yellow
	ColorCategoryChild = lipgloss.Color("10") // Bright Green

	// Table colors
	ColorTableBorder = lipgloss.Color("238")
)

// Spacing constants
const (
	PaddingSmall  = 1
	PaddingMedium = 2
	PaddingLarge  = 4
)

// Base styles that can be composed
var (
	BaseTextStyle = lipgloss.NewStyle()

	BoldStyle = lipgloss.NewStyle().
			Bold(true)

	ItalicStyle = lipgloss.NewStyle().
			Italic(true)
)

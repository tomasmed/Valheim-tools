package main

import (
	"github.com/charmbracelet/lipgloss"
)

// Norse / Viking Color Palette
var (
	ColorGold      = lipgloss.Color("#F59E0B")
	ColorGoldLight = lipgloss.Color("#FBBF24")
	ColorTimber    = lipgloss.Color("#78350F")
	ColorFjordCyan = lipgloss.Color("#38BDF8")
	ColorOceanBlue = lipgloss.Color("#0284C7")
	ColorGreen     = lipgloss.Color("#10B981")
	ColorRed       = lipgloss.Color("#EF4444")
	ColorDarkBg    = lipgloss.Color("#0B0F17")
	ColorCardBg    = lipgloss.Color("#0F172A")
	ColorBorder    = lipgloss.Color("#334155")
	ColorMuted     = lipgloss.Color("#64748B")
	ColorText      = lipgloss.Color("#F8FAFC")
)

// Styles
var (
	// Header
	styleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorGold).
			Padding(0, 1)

	styleVersion = lipgloss.NewStyle().
			Foreground(ColorGoldLight).
			Bold(true)

	styleSubtitle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Italic(true)

	// Status Badges
	styleBadgeOnline = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#064E3B")).
				Background(ColorGreen).
				Padding(0, 1)

	styleBadgeOffline = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(ColorRed).
				Padding(0, 1)

	styleBadgeSupervised = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#0C4A6E")).
				Background(ColorFjordCyan).
				Padding(0, 1)

	// Card Containers
	styleCard = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Background(ColorCardBg).
			Padding(1, 2)

	styleCardAmber = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorTimber).
			Background(ColorCardBg).
			Padding(1, 2)

	// Join Code Hero Box
	styleJoinCodeHero = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorGoldLight).
				Padding(0, 1)

	styleJoinCodeLabel = lipgloss.NewStyle().
				Foreground(ColorMuted).
				Bold(true)

	// Labels and Values
	styleLabel = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Width(12)

	styleValue = lipgloss.NewStyle().
			Foreground(ColorText).
			Bold(true)

	styleRune = lipgloss.NewStyle().
			Foreground(ColorGold).
			Bold(true)

	// Event log categories
	styleCategoryPlayer = lipgloss.NewStyle().
				Foreground(ColorGreen).
				Bold(true)

	styleCategorySave = lipgloss.NewStyle().
				Foreground(ColorFjordCyan).
				Bold(true)

	styleCategoryCombat = lipgloss.NewStyle().
				Foreground(ColorRed).
				Bold(true)

	styleCategorySystem = lipgloss.NewStyle().
				Foreground(ColorGold).
				Bold(true)

	// Progress Gauge
	styleGaugeFilled = lipgloss.NewStyle().
				Foreground(ColorGold).
				Bold(true)

	styleGaugeEmpty = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#1E293B"))

	// Footer Help Bar
	styleHelpKey = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorGold)

	styleHelpDesc = lipgloss.NewStyle().
			Foreground(ColorMuted)

	styleToast = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorGreen).
			Background(lipgloss.Color("#064E3B")).
			Padding(0, 2)
)

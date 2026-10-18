package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tickMsg time.Time
type eventMsg ServerLogEvent
type toastClearMsg struct{}

// TUIModel represents the Bubble Tea state machine for Drakkar
type TUIModel struct {
	cfg          Config
	tracker      *StateTracker
	batcher      *Batcher
	supervisor   *Supervisor
	viewport     viewport.Model
	width        int
	height       int
	ready        bool
	toastMsg     string
	recentEvents []ServerLogEvent
	maxEvents    int
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewTUIModel creates and configures the TUI state model
func NewTUIModel(ctx context.Context, cancel context.CancelFunc, cfg Config, tracker *StateTracker, batcher *Batcher, supervisor *Supervisor) *TUIModel {
	m := &TUIModel{
		cfg:          cfg,
		tracker:      tracker,
		batcher:      batcher,
		supervisor:   supervisor,
		maxEvents:    100,
		recentEvents: make([]ServerLogEvent, 0, 100),
		ctx:          ctx,
		cancel:       cancel,
	}

	return m
}

func (m *TUIModel) Init() tea.Cmd {
	return tea.Batch(
		tea.Tick(time.Second, func(t time.Time) tea.Msg {
			return tickMsg(t)
		}),
	)
}

func (m *TUIModel) PushEvent(ev ServerLogEvent) {
	if len(m.recentEvents) >= m.maxEvents {
		m.recentEvents = m.recentEvents[1:]
	}
	m.recentEvents = append(m.recentEvents, ev)
}

func (m *TUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.cancel()
			return m, tea.Quit

		case "c", "C":
			server, _ := m.tracker.GetSnapshot()
			if server.JoinCode != "" {
				_ = clipboard.WriteAll(server.JoinCode)
				m.toastMsg = fmt.Sprintf("✓ Copied Join Code [%s] to clipboard!", server.JoinCode)
			} else {
				m.toastMsg = "⚠ No active Join Code yet (waiting for server boot)"
			}
			cmds = append(cmds, tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
				return toastClearMsg{}
			}))

		case "f", "F":
			m.batcher.TriggerFlush()
			m.toastMsg = "⚡ Telemetry batch flush triggered!"
			cmds = append(cmds, tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
				return toastClearMsg{}
			}))

		case "r", "R":
			if m.supervisor != nil && m.supervisor.IsManaged() {
				m.toastMsg = "🔄 Restarting Valheim dedicated server..."
				go func() {
					_ = m.supervisor.Restart(context.Background())
				}()
			} else {
				m.toastMsg = "⚠ Unmanaged mode: server restart only available with --wrap"
			}
			cmds = append(cmds, tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
				return toastClearMsg{}
			}))

		case "s", "S":
			if m.supervisor != nil && m.supervisor.IsManaged() {
				m.toastMsg = "🛑 Stopping Valheim dedicated server..."
				go func() {
					_ = m.supervisor.Stop()
				}()
			} else {
				m.toastMsg = "⚠ Unmanaged mode: server stop only available with --wrap"
			}
			cmds = append(cmds, tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
				return toastClearMsg{}
			}))
		}

	case eventMsg:
		m.PushEvent(ServerLogEvent(msg))
		m.updateViewportContent()

	case tickMsg:
		cmds = append(cmds, tea.Tick(time.Second, func(t time.Time) tea.Msg {
			return tickMsg(t)
		}))

	case toastClearMsg:
		m.toastMsg = ""

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if !m.ready {
			m.viewport = viewport.New(msg.Width-4, 8)
			m.ready = true
		} else {
			m.viewport.Width = msg.Width - 4
		}
		m.updateViewportContent()
	}

	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

func (m *TUIModel) updateViewportContent() {
	var lines []string
	for i := len(m.recentEvents) - 1; i >= 0; i-- {
		ev := m.recentEvents[i]
		catStyle := styleCategorySystem
		switch ev.Category {
		case CategoryPlayer:
			catStyle = styleCategoryPlayer
		case CategorySave:
			catStyle = styleCategorySave
		case CategoryCombat:
			catStyle = styleCategoryCombat
		}

		timeStr := ev.Timestamp
		if t, err := time.Parse(time.RFC3339, ev.Timestamp); err == nil {
			timeStr = t.Format("15:04:05")
		} else if len(timeStr) > 8 {
			timeStr = timeStr[:8]
		}

		badge := catStyle.Render(fmt.Sprintf("[%s]", strings.ToUpper(string(ev.Category))))
		line := fmt.Sprintf("%s %-10s %s", styleSubtitle.Render(timeStr), badge, ev.Message)
		lines = append(lines, line)
	}

	if len(lines) == 0 {
		m.viewport.SetContent(styleSubtitle.Render("Waiting for server events... Tailing logs."))
	} else {
		m.viewport.SetContent(strings.Join(lines, "\n"))
	}
}

func (m *TUIModel) View() string {
	if m.width == 0 {
		return "Initializing Drakkar TUI..."
	}

	server, players := m.tracker.GetSnapshot()
	stats := m.batcher.GetStats()

	// 1. Header Banner
	headerLeft := lipgloss.JoinHorizontal(
		lipgloss.Center,
		styleTitle.Render("🛶 DRAKKAR UNIVERSAL HELMSMAN"),
		styleVersion.Render("v1.1.0"),
	)

	statusBadge := styleBadgeOnline.Render("● STREAMING")
	if server.IsOnline != nil && !*server.IsOnline {
		statusBadge = styleBadgeOffline.Render("● DISCONNECTED")
	}

	headerRight := statusBadge
	if m.supervisor != nil && m.supervisor.IsManaged() {
		headerRight = lipgloss.JoinHorizontal(lipgloss.Center, styleBadgeSupervised.Render("SUPERVISED"), " ", statusBadge)
	}

	headerGap := m.width - lipgloss.Width(headerLeft) - lipgloss.Width(headerRight) - 4
	if headerGap < 1 {
		headerGap = 1
	}
	header := lipgloss.JoinHorizontal(lipgloss.Center, headerLeft, strings.Repeat(" ", headerGap), headerRight)
	headerBox := styleCard.Width(m.width - 4).Render(header)

	// 2. Middle Row: 3 Cards (Join Code, Process/Uptime, Telemetry)
	cardWidth := (m.width - 8) / 3
	if cardWidth < 26 {
		cardWidth = 26
	}

	// Card 1: Join Code
	joinCodeDisplay := "SEARCHING..."
	if server.JoinCode != "" {
		joinCodeDisplay = server.JoinCode
	}
	cardJoinCode := styleCardAmber.Width(cardWidth).Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			styleJoinCodeLabel.Render("PLAYFAB JOIN CODE"),
			styleJoinCodeHero.Render(fmt.Sprintf("[ %s ]", joinCodeDisplay)),
			styleSubtitle.Render("Press 'c' to copy to clipboard"),
		),
	)

	// Card 2: Process & Realm HUD
	var procState ProcessState = ProcessUnmanaged
	var procPid int
	var procUptime string = "00h 00m 00s"
	if m.supervisor != nil {
		procState, procPid, procUptime, _ = m.supervisor.GetInfo()
	}

	worldDisplay := "Unknown"
	if server.WorldName != "" {
		worldDisplay = server.WorldName
	}
	dayDisplay := "Day 1"
	if server.DayCount != nil {
		dayDisplay = fmt.Sprintf("Day %d", *server.DayCount)
	}

	cardProcess := styleCard.Width(cardWidth).Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			styleJoinCodeLabel.Render("VALHEIM REALM & PROCESS"),
			fmt.Sprintf("%s %s", styleLabel.Render("World:"), styleValue.Render(worldDisplay)),
			fmt.Sprintf("%s %s", styleLabel.Render("Timeline:"), styleRune.Render(dayDisplay)),
			fmt.Sprintf("%s %s", styleLabel.Render("Process:"), formatProcessStatus(procState, procPid)),
			fmt.Sprintf("%s %s", styleLabel.Render("Uptime:"), styleValue.Render(procUptime)),
		),
	)

	// Card 3: Telemetry Cargo / Shipper
	gauge := renderBatchGauge(stats.PendingEvents, stats.BatchSize)
	cardTelemetry := styleCard.Width(cardWidth).Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			styleJoinCodeLabel.Render("MEAD HALL CARGO SHIPPER"),
			fmt.Sprintf("%s %s", styleLabel.Render("Buffer:"), gauge),
			fmt.Sprintf("%s %s", styleLabel.Render("Flushed:"), styleValue.Render(fmt.Sprintf("%d batches (%d ev)", stats.TotalBatches, stats.TotalEvents))),
			fmt.Sprintf("%s %s", styleLabel.Render("Status:"), formatHttpStatus(stats.LastStatus)),
		),
	)

	topGrid := lipgloss.JoinHorizontal(lipgloss.Top, cardJoinCode, " ", cardProcess, " ", cardTelemetry)

	// 3. Active Warriors Roster Card
	warriorsContent := renderWarriorsRoster(players)
	warriorsBox := styleCard.Width(m.width - 4).Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			styleJoinCodeLabel.Render(fmt.Sprintf("⚔️ WARRIORS SEATED AT THE HEARTH (%d)", len(players))),
			warriorsContent,
		),
	)

	// 4. Shieldwall Log Viewport Card
	logBox := styleCard.Width(m.width - 4).Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			styleJoinCodeLabel.Render("📜 SHIELDWALL EVENT LOG (LIVE STREAM)"),
			m.viewport.View(),
		),
	)

	// 5. Footer & Toast
	helpKeys := lipgloss.JoinHorizontal(
		lipgloss.Left,
		styleHelpKey.Render("[c]"), styleHelpDesc.Render(" Copy Code  "),
		styleHelpKey.Render("[f]"), styleHelpDesc.Render(" Flush  "),
		styleHelpKey.Render("[r]"), styleHelpDesc.Render(" Restart  "),
		styleHelpKey.Render("[s]"), styleHelpDesc.Render(" Stop  "),
		styleHelpKey.Render("[q]"), styleHelpDesc.Render(" Dock & Quit"),
	)

	footer := helpKeys
	if m.toastMsg != "" {
		footer = lipgloss.JoinHorizontal(lipgloss.Center, helpKeys, "  ", styleToast.Render(m.toastMsg))
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		headerBox,
		topGrid,
		warriorsBox,
		logBox,
		footer,
	)
}

func formatProcessStatus(state ProcessState, pid int) string {
	switch state {
	case ProcessRunning:
		return styleCategoryPlayer.Render(fmt.Sprintf("● RUNNING (PID %d)", pid))
	case ProcessStarting:
		return styleCategorySystem.Render("● STARTING...")
	case ProcessRestart:
		return styleCategorySystem.Render("● RESTARTING...")
	case ProcessStopped:
		return styleSubtitle.Render("○ STOPPED")
	case ProcessCrashed:
		return styleCategoryCombat.Render("⚠ CRASHED")
	default:
		return styleSubtitle.Render("UNMANAGED (Tailing log)")
	}
}

func formatHttpStatus(status string) string {
	if strings.Contains(status, "200") || strings.Contains(status, "OK") {
		return styleCategoryPlayer.Render(status)
	}
	if strings.Contains(status, "ERR") {
		return styleCategoryCombat.Render(status)
	}
	return styleSubtitle.Render(status)
}

func renderBatchGauge(pending, max int) string {
	if max <= 0 {
		max = 25
	}
	barWidth := 10
	filled := (pending * barWidth) / max
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	bar := styleGaugeFilled.Render(strings.Repeat("█", filled)) + styleGaugeEmpty.Render(strings.Repeat("░", empty))
	return fmt.Sprintf("%s %d/%d", bar, pending, max)
}

func renderWarriorsRoster(players []PlayerSession) string {
	if len(players) == 0 {
		return styleSubtitle.Render("No warriors currently in the realm. The mead hall stands quiet.")
	}

	var rows []string
	for _, p := range players {
		durStr := "recently joined"
		if t, err := time.Parse(time.RFC3339, p.ConnectedAt); err == nil {
			dur := time.Since(t).Truncate(time.Minute)
			durStr = fmt.Sprintf("%dm online", int(dur.Minutes()))
		}
		runeSymbol := "ᚱ"
		row := fmt.Sprintf(" %s %s %s %s",
			styleRune.Render(runeSymbol),
			styleValue.Render(p.Name),
			styleSubtitle.Render(fmt.Sprintf("[%s]", durStr)),
			styleSubtitle.Render(fmt.Sprintf("(ZDO: %s)", p.CharacterZDOID)),
		)
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}

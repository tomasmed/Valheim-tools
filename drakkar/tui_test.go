package main

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTUI_InitAndRender(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := Config{
		DashboardURL: "https://valheim-dash.vercel.app",
		BatchSize:    25,
		BatchTimeSec: 3,
	}

	tracker := NewStateTracker()
	client := NewHTTPClient(cfg.DashboardURL, "")
	batcher := NewBatcher(client, tracker, cfg.BatchSize, cfg.BatchTimeSec)
	sup := NewSupervisor("", nil)

	model := NewTUIModel(ctx, cancel, cfg, tracker, batcher, sup)
	if model.Init() == nil {
		t.Errorf("expected Init() to return a tick cmd")
	}

	// Test PushEvent
	model.PushEvent(ServerLogEvent{
		ID:        "ev-1",
		Timestamp: time.Now().Format(time.RFC3339),
		Category:  CategoryPlayer,
		Message:   "Ragnar Lodbrok entered the hall",
	})

	if len(model.recentEvents) != 1 {
		t.Errorf("expected 1 recent event, got %d", len(model.recentEvents))
	}

	// Test View before WindowSize
	view0 := model.View()
	if !strings.Contains(view0, "Initializing") {
		t.Errorf("expected Initializing text before resize, got %s", view0)
	}

	// Send WindowSizeMsg
	m, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = m.(*TUIModel)

	view := model.View()
	if !strings.Contains(view, "DRAKKAR UNIVERSAL HELMSMAN") {
		t.Errorf("expected title in view, got: %s", view)
	}
	if !strings.Contains(view, "PLAYFAB JOIN CODE") {
		t.Errorf("expected Join Code section in view, got: %s", view)
	}
	if !strings.Contains(view, "WARRIORS SEATED AT THE HEARTH") {
		t.Errorf("expected Warriors section in view, got: %s", view)
	}
}

func TestTUI_RenderBatchGauge(t *testing.T) {
	gauge := renderBatchGauge(10, 25)
	if !strings.Contains(gauge, "10/25") {
		t.Errorf("expected gauge to contain '10/25', got: %s", gauge)
	}
}

func TestTUI_WarriorsRoster(t *testing.T) {
	empty := renderWarriorsRoster(nil)
	if !strings.Contains(empty, "quiet") {
		t.Errorf("expected quiet empty hall message, got: %s", empty)
	}

	roster := renderWarriorsRoster([]PlayerSession{
		{
			Name:           "Bjorn",
			ConnectedAt:    time.Now().Format(time.RFC3339),
			CharacterZDOID: "100:1",
			IsOnline:       true,
		},
		{
			Name:           "HistoricalGhost",
			ConnectedAt:    time.Now().Add(-24 * time.Hour).Format(time.RFC3339),
			CharacterZDOID: "200:1",
			IsOnline:       false,
		},
	})
	if !strings.Contains(roster, "Bjorn") {
		t.Errorf("expected roster to contain Bjorn, got: %s", roster)
	}
	if strings.Contains(roster, "HistoricalGhost") {
		t.Errorf("expected roster to exclude offline HistoricalGhost, got: %s", roster)
	}
}

func TestTUI_KeyTriggers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := Config{BatchSize: 25, BatchTimeSec: 3}
	tracker := NewStateTracker()
	client := NewHTTPClient("http://127.0.0.1:9999", "")
	batcher := NewBatcher(client, tracker, cfg.BatchSize, cfg.BatchTimeSec)
	sup := NewSupervisor("", nil)

	model := NewTUIModel(ctx, cancel, cfg, tracker, batcher, sup)
	m, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = m.(*TUIModel)

	// Press 'f' (flush)
	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	model = m.(*TUIModel)
	if !strings.Contains(model.toastMsg, "flush") {
		t.Errorf("expected toast for flush, got: %s", model.toastMsg)
	}

	// Press 'c' (copy code)
	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	model = m.(*TUIModel)
	if model.toastMsg == "" {
		t.Errorf("expected toast for copy code, got empty")
	}

	// Press 'q' (quit)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Errorf("expected quit cmd on 'q'")
	}
}

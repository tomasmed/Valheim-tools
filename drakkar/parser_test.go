package main

import (
	"testing"
	"time"
)

func TestStateTracker_ProcessLine_JoinCode(t *testing.T) {
	tracker := NewStateTracker()
	now := time.Now()

	lines := []string{
		"09/12/2026 10:00:00: PlayFab session started. join code 987654",
		"09/12/2026 10:05:00: Join Code : 123456",
	}

	events := tracker.ProcessLine(lines[0], now)
	if len(events) == 0 {
		t.Fatalf("expected event for join code, got 0")
	}
	if tracker.Server.JoinCode != "987654" {
		t.Errorf("expected join code '987654', got '%s'", tracker.Server.JoinCode)
	}

	events2 := tracker.ProcessLine(lines[1], now)
	if len(events2) == 0 {
		t.Fatalf("expected event for rotated join code, got 0")
	}
	if tracker.Server.JoinCode != "123456" {
		t.Errorf("expected join code '123456', got '%s'", tracker.Server.JoinCode)
	}
}

func TestStateTracker_ProcessLine_VersionAndWorld(t *testing.T) {
	tracker := NewStateTracker()
	now := time.Now()

	tracker.ProcessLine("09/12/2026 10:00:00: Valheim version: 0.219.16", now)
	if tracker.Server.Version != "0.219.16" {
		t.Errorf("expected version '0.219.16', got '%s'", tracker.Server.Version)
	}

	tracker.ProcessLine("09/12/2026 10:00:01: ZNet.LoadWorld: Wowosi", now)
	if tracker.Server.WorldName != "Wowosi" {
		t.Errorf("expected world name 'Wowosi', got '%s'", tracker.Server.WorldName)
	}

	events := tracker.ProcessLine("09/12/2026 10:00:02: Day 42", now)
	if len(events) == 0 {
		t.Errorf("expected event for day counter")
	}
	if tracker.Server.DayCount == nil || *tracker.Server.DayCount != 42 {
		t.Errorf("expected day 42, got %v", tracker.Server.DayCount)
	}
}

func TestStateTracker_ProcessLine_PlayerLifecycle(t *testing.T) {
	tracker := NewStateTracker()
	now := time.Now()

	// 1. Player Login
	loginLine := "09/12/2026 10:10:00: Got character ZDOID from Bjorn Ironside : 99887766:1"
	events := tracker.ProcessLine(loginLine, now)
	if len(events) == 0 {
		t.Fatalf("expected login event, got 0")
	}
	if events[0].Category != CategoryPlayer {
		t.Errorf("expected category 'player', got '%s'", events[0].Category)
	}

	server, players := tracker.GetSnapshot()
	if server.CurrentPlayers == nil || *server.CurrentPlayers != 1 {
		t.Errorf("expected 1 current player, got %v", server.CurrentPlayers)
	}
	if len(players) != 1 || !players[0].IsOnline || players[0].Name != "Bjorn Ironside" {
		t.Errorf("expected active player 'Bjorn Ironside', got %+v", players)
	}

	// 2. Player Logout
	logoutLine := "09/12/2026 10:45:00: Destroying abandoned non persistent zdo 99887766:1"
	events = tracker.ProcessLine(logoutLine, now)
	if len(events) == 0 {
		t.Fatalf("expected logout event, got 0")
	}

	server, players = tracker.GetSnapshot()
	if server.CurrentPlayers == nil || *server.CurrentPlayers != 0 {
		t.Errorf("expected 0 current players, got %v", server.CurrentPlayers)
	}
	if len(players) != 1 || players[0].IsOnline {
		t.Errorf("expected player to be offline, got %+v", players[0])
	}
}

func TestStateTracker_ProcessLine_WorldSave(t *testing.T) {
	tracker := NewStateTracker()
	now := time.Now()

	saveLine := "09/12/2026 11:00:00: World saved ( 45.2ms )"
	events := tracker.ProcessLine(saveLine, now)
	if len(events) == 0 {
		t.Fatalf("expected save event, got 0")
	}
	if events[0].Category != CategorySave || events[0].Level != LevelSuccess {
		t.Errorf("expected save success event, got %+v", events[0])
	}
	if tracker.Server.LastSavedAt == "" {
		t.Errorf("expected LastSavedAt to be populated")
	}
}

func TestStateTracker_ProcessLine_SteamNegativeZDOID(t *testing.T) {
	tracker := NewStateTracker()
	now := time.Now()

	// 1. Steam Player Login with negative ZDOID and clan brackets/hyphen
	loginLine := "09/15/2026 12:00:00: Got character ZDOID from Slyder-sno [Clan] : -123345346346345:1"
	events := tracker.ProcessLine(loginLine, now)
	if len(events) == 0 {
		t.Fatalf("expected login event for steam user with negative ZDOID, got 0")
	}

	server, players := tracker.GetSnapshot()
	if server.CurrentPlayers == nil || *server.CurrentPlayers != 1 {
		t.Errorf("expected 1 current player, got %v", server.CurrentPlayers)
	}
	if len(players) != 1 || !players[0].IsOnline || players[0].Name != "Slyder-sno [Clan]" {
		t.Errorf("expected active player 'Slyder-sno [Clan]', got %+v", players)
	}
	if players[0].CharacterZDOID != "-123345346346345:1" {
		t.Errorf("expected CharacterZDOID '-123345346346345:1', got '%s'", players[0].CharacterZDOID)
	}

	// 2. Steam Player Logout with negative ZDOID
	logoutLine := "09/15/2026 12:30:00: Destroying abandoned non persistent zdo -123345346346345:1"
	events = tracker.ProcessLine(logoutLine, now)
	if len(events) == 0 {
		t.Fatalf("expected logout event for steam user with negative ZDOID, got 0")
	}

	server, players = tracker.GetSnapshot()
	if server.CurrentPlayers == nil || *server.CurrentPlayers != 0 {
		t.Errorf("expected 0 current players after logout, got %v", server.CurrentPlayers)
	}
	if len(players) != 1 || players[0].IsOnline {
		t.Errorf("expected player to be marked offline, got %+v", players[0])
	}
}


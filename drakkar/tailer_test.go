package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTailer_CursorSaveAndRead(t *testing.T) {
	tempDir := t.TempDir()
	cursorPath := filepath.Join(tempDir, ".drakkar.cursor")

	tailer := NewTailer("dummy.log", cursorPath, 50)
	if offset := tailer.readCursor(); offset != 0 {
		t.Errorf("expected 0 for non-existent cursor, got %d", offset)
	}

	if err := tailer.saveCursor(12345); err != nil {
		t.Fatalf("failed to save cursor: %v", err)
	}

	if offset := tailer.readCursor(); offset != 12345 {
		t.Errorf("expected 12345, got %d", offset)
	}
}

func TestTailer_StreamingAndRotation(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "server.log")
	cursorPath := filepath.Join(tempDir, ".drakkar.cursor")

	// 1. Pre-populate file
	initialContent := "Line 1: Server start\nLine 2: Loading world\n"
	if err := os.WriteFile(logPath, []byte(initialContent), 0644); err != nil {
		t.Fatalf("failed to write log file: %v", err)
	}

	tailer := NewTailer(logPath, cursorPath, 50)
	linesCh := make(chan string, 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = tailer.Start(ctx, linesCh)
	}()

	// Verify pre-existing lines are read
	select {
	case l1 := <-linesCh:
		if l1 != "Line 1: Server start" {
			t.Errorf("unexpected l1: %s", l1)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for line 1")
	}

	select {
	case l2 := <-linesCh:
		if l2 != "Line 2: Loading world" {
			t.Errorf("unexpected l2: %s", l2)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for line 2")
	}

	// 2. Append new line
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("failed to open file for append: %v", err)
	}
	_, _ = f.WriteString("Line 3: Valheim version: 0.219.16\n")
	_ = f.Close()

	select {
	case l3 := <-linesCh:
		if l3 != "Line 3: Valheim version: 0.219.16" {
			t.Errorf("unexpected l3: %s", l3)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for appended line 3")
	}

	cancel()
}

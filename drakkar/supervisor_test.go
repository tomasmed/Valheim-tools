package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSupervisor_Unmanaged(t *testing.T) {
	sup := NewSupervisor("", nil)
	if sup.IsManaged() {
		t.Errorf("expected supervisor to be unmanaged")
	}

	state, pid, uptime, path, attached := sup.GetInfo()
	if attached {
		if state != ProcessRunning || pid <= 0 {
			t.Errorf("expected attached process to be running with pid, got %v (pid %d)", state, pid)
		}
	} else {
		if state != ProcessUnmanaged {
			t.Errorf("expected state UNMANAGED, got %v", state)
		}
		if pid != 0 {
			t.Errorf("expected pid 0, got %d", pid)
		}
	}

	if uptime == "" {
		t.Errorf("expected non-empty uptime string")
	}
	if path != "" {
		t.Errorf("expected empty path, got %s", path)
	}

	if err := sup.Start(context.Background()); err != nil {
		t.Errorf("unexpected start error for unmanaged supervisor: %v", err)
	}
	if err := sup.Stop(); err != nil {
		t.Errorf("unexpected stop error for unmanaged supervisor: %v", err)
	}
}

func TestSupervisor_ManagedExecution(t *testing.T) {
	tmpDir := t.TempDir()
	logCh := make(chan string, 10)

	var scriptPath string
	if runtime.GOOS == "windows" {
		scriptPath = filepath.Join(tmpDir, "test_server.bat")
		err := os.WriteFile(scriptPath, []byte("@echo off\r\necho TEST SERVER STARTED\r\nping -n 3 127.0.0.1 >nul\r\necho TEST SERVER STOPPED\r\n"), 0755)
		if err != nil {
			t.Fatalf("failed to create test bat file: %v", err)
		}
	} else {
		scriptPath = filepath.Join(tmpDir, "test_server.sh")
		err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho 'TEST SERVER STARTED'\nsleep 2\necho 'TEST SERVER STOPPED'\n"), 0755)
		if err != nil {
			t.Fatalf("failed to create test sh file: %v", err)
		}
	}

	sup := NewSupervisor(scriptPath, logCh)
	if !sup.IsManaged() {
		t.Fatalf("expected supervisor to be managed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sup.Start(ctx); err != nil {
		t.Fatalf("failed to start supervisor: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	state, pid, uptime, path, attached := sup.GetInfo()
	if state != ProcessRunning {
		t.Errorf("expected state RUNNING, got %v", state)
	}
	if attached {
		t.Errorf("expected managed supervisor not to be attached")
	}
	if pid <= 0 {
		t.Errorf("expected valid PID > 0, got %d", pid)
	}
	if path != scriptPath {
		t.Errorf("expected path %s, got %s", scriptPath, path)
	}
	if uptime == "" {
		t.Errorf("expected non-empty uptime")
	}

	// Stop cleanly
	if err := sup.Stop(); err != nil {
		t.Errorf("failed to stop supervisor: %v", err)
	}
}

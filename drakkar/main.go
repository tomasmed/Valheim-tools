package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const version = "1.0.0"

func main() {
	cfg := LoadConfig()

	printBanner(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tracker := NewStateTracker()
	client := NewHTTPClient(cfg.DashboardURL, cfg.Secret)

	// Catch-up phase if cursor file is empty or missing
	performCatchUp(cfg, tracker, client)

	linesCh := make(chan string, 200)
	batcher := NewBatcher(client, tracker, cfg.BatchSize, cfg.BatchTimeSec)
	tailer := NewTailer(cfg.LogPath, cfg.CursorPath, cfg.PollIntervalMs)

	// Run batcher in goroutine
	go batcher.Run(ctx, linesCh)

	// Start tailer (blocks until context cancellation or error)
	log.Printf("[Drakkar] Initialized live streaming daemon. Monitoring for updates...")
	if err := tailer.Start(ctx, linesCh); err != nil {
		log.Printf("[Drakkar] Tailer stopped with error: %v", err)
	}

	close(linesCh)
	log.Printf("[Drakkar] Drakkar shipper successfully docked. Skål!")
}

func printBanner(cfg Config) {
	fmt.Println("==========================================================")
	fmt.Printf(" 🛶 Drakkar Universal Log Shipper v%s (Go)\n", version)
	fmt.Println(" Open-Source Tools: https://github.com/tomasmed/Valheim-tools")
	fmt.Println("==========================================================")
	fmt.Printf("Dashboard:    %s/api/telemetry\n", strings.TrimRight(cfg.DashboardURL, "/"))
	fmt.Printf("Log Target:   %s\n", cfg.LogPath)
	if cfg.Secret != "" {
		masked := cfg.Secret
		if len(masked) > 8 {
			masked = masked[:8] + "..."
		}
		fmt.Printf("Security:     Bearer Authentication Active (%s)\n", masked)
	} else {
		fmt.Println("Security:     No Secret Token provided (local/public mode)")
	}
	fmt.Printf("Batching:     %d lines / %ds debounce\n", cfg.BatchSize, cfg.BatchTimeSec)
	fmt.Printf("Cursor File:  %s\n", cfg.CursorPath)
	fmt.Println("==========================================================")
}

func performCatchUp(cfg Config, tracker *StateTracker, client *HTTPClient) {
	// If cursor file already exists and has valid offset, skip catch-up
	if _, err := os.Stat(cfg.CursorPath); err == nil {
		data, err := os.ReadFile(cfg.CursorPath)
		if err == nil && len(strings.TrimSpace(string(data))) > 0 {
			return
		}
	}

	// Check if log file exists
	file, err := os.Open(cfg.LogPath)
	if err != nil {
		return
	}
	defer file.Close()

	log.Printf("[Drakkar] Performing initial catch-up scan on %s...", cfg.LogPath)
	reader := bufio.NewReader(file)
	var totalBytes int64

	for {
		line, err := reader.ReadString('\n')
		totalBytes += int64(len(line))
		cleanLine := strings.TrimRight(line, "\r\n")
		if cleanLine != "" {
			_ = tracker.ProcessLine(cleanLine, time.Now())
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("[Drakkar] Catch-up read error: %v", err)
			break
		}
	}

	server, players := tracker.GetSnapshot()
	log.Printf("[Drakkar] Catch-up complete (%d bytes). JoinCode: '%s', World: '%s', Version: '%s', Players: %d",
		totalBytes, server.JoinCode, server.WorldName, server.Version, len(players))

	// Send initial snapshot
	payload := TelemetryPayload{
		Server:  &server,
		Players: players,
	}
	if err := client.SendTelemetry(context.Background(), payload); err != nil {
		log.Printf("[Drakkar] Initial telemetry sync failed: %v", err)
	} else {
		log.Printf("[Drakkar] Initial state synced with Mead Hall.")
	}

	// Save initial cursor
	_ = os.WriteFile(cfg.CursorPath, []byte(fmt.Sprintf("%d", totalBytes)), 0644)
}

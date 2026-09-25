package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const version = "1.0.0"

func main() {
	cfg := LoadConfig()

	if cfg.ShowVersion {
		fmt.Printf("Drakkar Universal Log Shipper v%s\n", version)
		os.Exit(0)
	}

	if cfg.CheckUpdate {
		fmt.Printf("Checking for updates (current version: v%s)...\n", version)
		latest, err := queryLatestRelease()
		if err != nil {
			fmt.Printf("Update check failed: %v\n", err)
			os.Exit(1)
		}
		if latest != "" && isNewerVersion(version, latest) {
			fmt.Printf("A newer version of Drakkar is available: v%s (current: v%s)\n", latest, version)
			fmt.Println("Download latest release at: https://github.com/tomasmed/Valheim-tools/releases")
		} else {
			fmt.Printf("Drakkar is up to date (v%s).\n", version)
		}
		os.Exit(0)
	}

	printBanner(cfg)

	// Non-blocking background check for newer releases
	go func() {
		latest, err := queryLatestRelease()
		if err == nil && latest != "" && isNewerVersion(version, latest) {
			log.Printf("[Drakkar] 🔔 Notice: A newer version of Drakkar (v%s) is available at https://github.com/tomasmed/Valheim-tools/releases", latest)
		}
	}()

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
	var savedCursor int64 = -1
	if _, err := os.Stat(cfg.CursorPath); err == nil {
		data, err := os.ReadFile(cfg.CursorPath)
		if err == nil {
			if val, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); err == nil && val > 0 {
				savedCursor = val
			}
		}
	}

	// Check if log file exists
	file, err := os.Open(cfg.LogPath)
	if err != nil {
		return
	}
	defer file.Close()

	log.Printf("[Drakkar] Scanning %s to initialize server state...", cfg.LogPath)
	reader := bufio.NewReader(file)
	var totalBytes int64

	for {
		if savedCursor > 0 && totalBytes >= savedCursor {
			break
		}
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
	log.Printf("[Drakkar] State restored (%d bytes scanned). JoinCode: '%s', World: '%s', Version: '%s', Players: %d",
		totalBytes, server.JoinCode, server.WorldName, server.Version, len(players))

	// Send initial snapshot to Mead Hall
	payload := TelemetryPayload{
		Server:  &server,
		Players: players,
	}
	if err := client.SendTelemetry(context.Background(), payload); err != nil {
		log.Printf("[Drakkar] Initial telemetry sync failed: %v", err)
	} else {
		log.Printf("[Drakkar] Initial state synced with Mead Hall.")
	}

	// If no previous cursor existed, save the new cursor
	if savedCursor <= 0 {
		_ = os.WriteFile(cfg.CursorPath, []byte(fmt.Sprintf("%d", totalBytes)), 0644)
	}
}

type githubRelease struct {
	TagName string `json:"tag_name"`
}

func queryLatestRelease() (string, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequest("GET", "https://api.github.com/repos/tomasmed/Valheim-tools/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Drakkar-Shipper/"+version)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	return strings.TrimPrefix(rel.TagName, "v"), nil
}

func isNewerVersion(current, latest string) bool {
	cParts := strings.Split(strings.TrimPrefix(current, "v"), ".")
	lParts := strings.Split(strings.TrimPrefix(latest, "v"), ".")
	for i := 0; i < len(cParts) && i < len(lParts); i++ {
		cNum, _ := strconv.Atoi(cParts[i])
		lNum, _ := strconv.Atoi(lParts[i])
		if lNum > cNum {
			return true
		}
		if lNum < cNum {
			return false
		}
	}
	return len(lParts) > len(cParts)
}


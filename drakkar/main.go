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

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

const version = "1.1.0"

func main() {
	cfg := LoadConfig()

	if cfg.ShowVersion {
		fmt.Printf("Drakkar Universal Log Shipper & Helmsman v%s\n", version)
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

	// Prevent duplicate background process if wrapped bat script still has a legacy 'start drakkar.exe' call
	if os.Getenv("DRAKKAR_SUPERVISED") == "1" && cfg.WrapPath == "" {
		os.Exit(0)
	}

	// Non-blocking background check for newer releases
	go func() {
		latest, err := queryLatestRelease()
		if err == nil && latest != "" && isNewerVersion(version, latest) {
			log.Printf("[Drakkar] 🔔 Notice: A newer version of Drakkar (v%s) is available at https://github.com/tomasmed/Valheim-tools/releases", latest)
		}
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	isInteractive := !cfg.NoTUI && term.IsTerminal(int(os.Stdout.Fd()))
	if isInteractive {
		fmt.Print("\033]0;🛶 Drakkar Helmsman\007")
	}

	tracker := NewStateTracker()
	client := NewHTTPClient(cfg.DashboardURL, cfg.Secret)

	linesCh := make(chan string, 200)

	if cfg.Demo {
		initDemoState(tracker, linesCh, ctx)
	} else {
		// Catch-up phase if cursor file is empty or missing
		performCatchUp(cfg, tracker, client)
	}

	batcher := NewBatcher(client, tracker, cfg.BatchSize, cfg.BatchTimeSec)
	tailer := NewTailer(cfg.LogPath, cfg.CursorPath, cfg.PollIntervalMs)
	supervisor := NewSupervisor(cfg.WrapPath, linesCh)

	// Start supervisor if executable is specified
	if supervisor.IsManaged() {
		go func() {
			if err := supervisor.Start(ctx); err != nil {
				log.Printf("[Drakkar Supervisor] Error starting server process: %v", err)
			}
		}()
		// Ensure child process terminates when Drakkar terminates
		defer func() {
			_ = supervisor.Stop()
		}()
	}

	// Run batcher in goroutine
	go batcher.Run(ctx, linesCh)

	// Run tailer in goroutine
	go func() {
		if err := tailer.Start(ctx, linesCh); err != nil {
			log.Printf("[Drakkar] Tailer stopped with error: %v", err)
		}
	}()

	if isInteractive {
		// In interactive TUI mode, route logs away from stdout to avoid corrupting alt screen
		log.SetOutput(io.Discard)

		tuiModel := NewTUIModel(ctx, stop, cfg, tracker, batcher, supervisor)
		tuiProg := tea.NewProgram(tuiModel, tea.WithAltScreen())

		batcher.SetEventCallback(func(ev ServerLogEvent) {
			tuiProg.Send(eventMsg(ev))
		})

		if _, err := tuiProg.Run(); err != nil {
			fmt.Printf("Error running Drakkar TUI: %v\n", err)
		}
	} else {
		// Headless mode (Docker, systemd, or --no-tui)
		printBanner(cfg)
		log.Printf("[Drakkar] Running in headless mode. Monitoring logs...")
		<-ctx.Done()
		log.Printf("[Drakkar] Shutdown signal received.")
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
	if cfg.WrapPath != "" {
		fmt.Printf("Supervising:  %s\n", cfg.WrapPath)
	}
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

	file, err := os.Open(cfg.LogPath)
	if err != nil {
		return
	}
	defer file.Close()

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
			break
		}
	}

	server, players := tracker.GetSnapshot()

	payload := TelemetryPayload{
		Server:  &server,
		Players: players,
	}
	_ = client.SendTelemetry(context.Background(), payload)

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

func initDemoState(tracker *StateTracker, linesCh chan string, ctx context.Context) {
	online := true
	day := 142
	two := 2
	tracker.mu.Lock()
	tracker.Server = ServerStatus{
		IsOnline:       &online,
		JoinCode:       "748192",
		WorldName:      "Wowosi",
		Version:        "0.218.15",
		DayCount:       &day,
		CurrentPlayers: &two,
	}
	tracker.ActivePlayers["Ragnar Lodbrok"] = &PlayerSession{
		ID:             "p-1",
		Name:           "Ragnar Lodbrok",
		ConnectedAt:    time.Now().Add(-42 * time.Minute).Format(time.RFC3339),
		CharacterZDOID: "10482:1",
		IsOnline:       true,
	}
	tracker.ActivePlayers["Lagertha"] = &PlayerSession{
		ID:             "p-2",
		Name:           "Lagertha",
		ConnectedAt:    time.Now().Add(-18 * time.Minute).Format(time.RFC3339),
		CharacterZDOID: "10485:2",
		IsOnline:       true,
	}
	tracker.mu.Unlock()

	// Feed mock events periodically
	go func() {
		demoLines := []string{
			"World saved ( 128.4ms )",
			"Got character ZDOID from Bjorn Ironside : 10499:3",
			"Day 143",
			"World saved ( 112.1ms )",
			"Got character ZDOID from Freydis : 10512:4",
			"Destroying abandoned non persistent zdo 10485:2",
			"World saved ( 145.8ms )",
		}
		idx := 0
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if idx < len(demoLines) {
					select {
					case linesCh <- demoLines[idx]:
						idx++
					default:
					}
				}
			}
		}
	}()
}

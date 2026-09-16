package main

import (
	"flag"
	"os"
	"path/filepath"
	"strconv"
)

// Config holds all runtime settings for Drakkar
type Config struct {
	DashboardURL   string
	Secret         string
	LogPath        string
	BatchSize      int
	BatchTimeSec   int
	CursorPath     string
	PollIntervalMs int
}

// LoadConfig parses flags, checks environment variables, and resolves defaults
func LoadConfig() Config {
	var cfg Config

	flag.StringVar(&cfg.DashboardURL, "url", getEnv("DRAKKAR_DASHBOARD_URL", "https://valheim-dash.vercel.app"), "Target Valheim Mead Hall dashboard URL")
	flag.StringVar(&cfg.Secret, "secret", getEnvMulti([]string{"DRAKKAR_SECRET", "TELEMETRY_SECRET"}, ""), "Realm server secret token")
	flag.StringVar(&cfg.LogPath, "log", getEnv("DRAKKAR_LOG_PATH", ""), "Path to Valheim server.log or Player.log")
	
	defaultBatchSize := getEnvInt("DRAKKAR_BATCH_SIZE", 25)
	flag.IntVar(&cfg.BatchSize, "batch-size", defaultBatchSize, "Maximum lines before forced flush")

	defaultBatchTime := getEnvInt("DRAKKAR_BATCH_TIME_SEC", 3)
	flag.IntVar(&cfg.BatchTimeSec, "batch-time", defaultBatchTime, "Maximum seconds before forced flush")

	flag.StringVar(&cfg.CursorPath, "cursor", getEnv("DRAKKAR_CURSOR_PATH", "./.drakkar.cursor"), "Location of cursor offset file")

	defaultPollInterval := getEnvInt("DRAKKAR_POLL_INTERVAL_MS", 500)
	flag.IntVar(&cfg.PollIntervalMs, "poll-interval", defaultPollInterval, "Log poll interval in milliseconds")

	flag.Parse()

	if cfg.LogPath == "" {
		cfg.LogPath = resolveDefaultLogPath()
	}

	return cfg
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}

func getEnvMulti(keys []string, defaultVal string) string {
	for _, k := range keys {
		if val, ok := os.LookupEnv(k); ok && val != "" {
			return val
		}
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

// resolveDefaultLogPath checks common locations across Linux, Windows, and Docker
func resolveDefaultLogPath() string {
	candidates := []string{}

	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		candidates = append(candidates,
			filepath.Join(home, ".config", "unity3d", "IronGate", "Valheim", "server.log"),
			filepath.Join(home, ".config", "unity3d", "IronGate", "Valheim", "Player.log"),
			filepath.Join(home, "AppData", "LocalLow", "IronGate", "Valheim", "server.log"),
			filepath.Join(home, "AppData", "LocalLow", "IronGate", "Valheim", "Player.log"),
		)
	}

	candidates = append(candidates,
		"/opt/valheim/server.log",
		"/valheim/server.log",
		"/config/logs/server.log",
		"/logs/valheim_server.log",
		"./Player.log",
		"./server.log",
	)

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Default fallback
	return "./Player.log"
}

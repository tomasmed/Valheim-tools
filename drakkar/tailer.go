package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Tailer watches a log file and emits lines to a channel
type Tailer struct {
	logPath        string
	cursorPath     string
	pollIntervalMs int
	offset         int64
}

// NewTailer creates a new Tailer instance
func NewTailer(logPath, cursorPath string, pollIntervalMs int) *Tailer {
	return &Tailer{
		logPath:        logPath,
		cursorPath:     cursorPath,
		pollIntervalMs: pollIntervalMs,
	}
}

// readCursor reads the persisted byte offset from the cursor file
func (t *Tailer) readCursor() int64 {
	if t.cursorPath == "" {
		return 0
	}
	data, err := os.ReadFile(t.cursorPath)
	if err != nil {
		return 0
	}
	val, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || val < 0 {
		return 0
	}
	return val
}

// saveCursor writes the current byte offset to the cursor file atomically
func (t *Tailer) saveCursor(offset int64) error {
	if t.cursorPath == "" {
		return nil
	}
	dir := filepath.Dir(t.cursorPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	tmpFile := t.cursorPath + ".tmp"
	if err := os.WriteFile(tmpFile, []byte(strconv.FormatInt(offset, 10)), 0644); err != nil {
		return err
	}
	return os.Rename(tmpFile, t.cursorPath)
}

// Start begins tailing the log file and sends complete lines to linesCh
func (t *Tailer) Start(ctx context.Context, linesCh chan<- string) error {
	t.offset = t.readCursor()

	pollDuration := time.Duration(t.pollIntervalMs) * time.Millisecond
	if pollDuration < 50*time.Millisecond {
		pollDuration = 50 * time.Millisecond
	}

	// 1. Wait for log file to exist
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if _, err := os.Stat(t.logPath); err == nil {
			break
		}
		log.Printf("[Drakkar] Waiting for log file to appear at %s...", t.logPath)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(3 * time.Second):
		}
	}

	file, err := os.Open(t.logPath)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat log file: %w", err)
	}

	// Check rotation / truncation
	if stat.Size() < t.offset {
		log.Printf("[Drakkar] Log file truncated or rotated (size %d < cursor %d). Resetting cursor to 0.", stat.Size(), t.offset)
		t.offset = 0
	}

	if _, err := file.Seek(t.offset, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek in log file: %w", err)
	}

	log.Printf("[Drakkar] Tailing %s from offset %d bytes", t.logPath, t.offset)

	reader := bufio.NewReader(file)
	ticker := time.NewTicker(pollDuration)
	defer ticker.Stop()

	cursorSaveTicker := time.NewTicker(5 * time.Second)
	defer cursorSaveTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = t.saveCursor(t.offset)
			return nil

		case <-cursorSaveTicker.C:
			_ = t.saveCursor(t.offset)

		default:
			line, err := reader.ReadString('\n')
			if err == nil {
				// Successfully read a full line ending with \n
				t.offset += int64(len(line))
				cleanLine := strings.TrimRight(line, "\r\n")
				if cleanLine != "" {
					linesCh <- cleanLine
				}
			} else if err == io.EOF {
				// Incomplete line or end of file
				// Check for log rotation / truncation
				curStat, statErr := os.Stat(t.logPath)
				if statErr == nil {
					if curStat.Size() < t.offset {
						log.Printf("[Drakkar] Log file rotated or truncated. Resetting cursor to 0.")
						_ = file.Close()
						newFile, openErr := os.Open(t.logPath)
						if openErr == nil {
							file = newFile
							reader = bufio.NewReader(file)
							t.offset = 0
							_ = t.saveCursor(0)
						}
					}
				}

				select {
				case <-ctx.Done():
					_ = t.saveCursor(t.offset)
					return nil
				case <-ticker.C:
					// Check if more data was written
					continue
				}
			} else {
				// Other error reading file
				log.Printf("[Drakkar] Read error: %v. Retrying in %v...", err, pollDuration)
				time.Sleep(pollDuration)
			}
		}
	}
}

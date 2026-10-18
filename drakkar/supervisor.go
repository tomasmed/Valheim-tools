package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProcessState represents current state of the supervised server process
type ProcessState string

const (
	ProcessUnmanaged ProcessState = "UNMANAGED"
	ProcessStarting  ProcessState = "STARTING"
	ProcessRunning   ProcessState = "RUNNING"
	ProcessStopped   ProcessState = "STOPPED"
	ProcessCrashed   ProcessState = "CRASHED"
	ProcessRestart   ProcessState = "RESTARTING"
)

// Supervisor manages an external server process (like valheim_server.exe or start_wowosi.bat)
type Supervisor struct {
	mu          sync.RWMutex
	execPath    string
	args        []string
	cmd         *exec.Cmd
	state       ProcessState
	startTime   time.Time
	pid         int
	attached    bool
	exitErr     error
	outputLines []string
	maxLines    int
	logCh       chan<- string
}

// NewSupervisor initializes a supervisor for the given executable path
func NewSupervisor(execPath string, logCh chan<- string) *Supervisor {
	state := ProcessUnmanaged
	attached := false
	pid := 0
	var startTime time.Time

	if execPath != "" {
		state = ProcessStopped
	} else {
		// Attempt to auto-detect already running valheim_server
		if runningPid := findRunningServerPid(); runningPid > 0 {
			state = ProcessRunning
			pid = runningPid
			attached = true
			startTime = time.Now()
		}
	}

	return &Supervisor{
		execPath:    execPath,
		state:       state,
		pid:         pid,
		attached:    attached,
		startTime:   startTime,
		maxLines:    500,
		outputLines: make([]string, 0, 500),
		logCh:       logCh,
	}
}

func findRunningServerPid() int {
	if runtime.GOOS == "windows" {
		out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq valheim_server.exe", "/FO", "CSV", "/NH").Output()
		if err == nil {
			line := strings.TrimSpace(string(out))
			if strings.Contains(line, "valheim_server") {
				parts := strings.Split(line, ",")
				if len(parts) >= 2 {
					pidStr := strings.Trim(parts[1], "\" ")
					if p, err := strconv.Atoi(pidStr); err == nil && p > 0 {
						return p
					}
				}
			}
		}
	} else {
		out, err := exec.Command("pgrep", "-f", "valheim_server").Output()
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) > 0 {
				if p, err := strconv.Atoi(lines[0]); err == nil && p > 0 {
					return p
				}
			}
		}
	}
	return 0
}

// IsManaged returns true if an executable was configured for Drakkar to manage
func (s *Supervisor) IsManaged() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.execPath != ""
}

// IsAttached returns true if connected to an existing running process
func (s *Supervisor) IsAttached() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.attached
}

// Start launches the supervised server executable
func (s *Supervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.execPath == "" {
		s.mu.Unlock()
		return nil
	}

	absPath, err := filepath.Abs(s.execPath)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("invalid exec path: %w", err)
	}

	workDir := filepath.Dir(absPath)
	s.state = ProcessStarting
	s.attached = false
	s.mu.Unlock()

	var cmd *exec.Cmd
	ext := strings.ToLower(filepath.Ext(absPath))

	if runtime.GOOS == "windows" && (ext == ".bat" || ext == ".cmd") {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/c", absPath)
	} else if runtime.GOOS == "windows" && ext == ".ps1" {
		cmd = exec.CommandContext(ctx, "powershell.exe", "-ExecutionPolicy", "Bypass", "-File", absPath)
	} else {
		cmd = exec.CommandContext(ctx, absPath, s.args...)
	}

	cmd.Dir = workDir
	cmd.Env = os.Environ()
	// Ensure SteamAppId is set for Valheim dedicated servers
	cmd.Env = append(cmd.Env, "SteamAppId=892970", "DRAKKAR_SUPERVISED=1")

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		s.mu.Lock()
		s.state = ProcessCrashed
		s.exitErr = err
		s.mu.Unlock()
		return fmt.Errorf("failed to start server process: %w", err)
	}

	s.mu.Lock()
	s.cmd = cmd
	s.pid = cmd.Process.Pid
	s.state = ProcessRunning
	s.startTime = time.Now()
	s.mu.Unlock()

	// Capture output lines
	go s.pipeReader(stdout)
	go s.pipeReader(stderr)

	// Monitor lifecycle in background
	go func() {
		waitErr := cmd.Wait()
		s.mu.Lock()
		defer s.mu.Unlock()

		if s.state == ProcessRestart {
			// Restart in progress, do not mark crashed/stopped
			return
		}

		if waitErr != nil {
			s.state = ProcessCrashed
			s.exitErr = waitErr
			log.Printf("[Drakkar Supervisor] Process %d exited with error: %v", s.pid, waitErr)
		} else {
			s.state = ProcessStopped
			log.Printf("[Drakkar Supervisor] Process %d stopped cleanly", s.pid)
		}
	}()

	return nil
}

func (s *Supervisor) pipeReader(r io.Reader) {
	if r == nil {
		return
	}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		s.mu.Lock()
		if len(s.outputLines) >= s.maxLines {
			s.outputLines = s.outputLines[1:]
		}
		s.outputLines = append(s.outputLines, line)
		s.mu.Unlock()

		if s.logCh != nil {
			select {
			case s.logCh <- line:
			default:
			}
		}
	}
}

// Stop cleanly terminates the supervised server process (or detaches if unmanaged)
func (s *Supervisor) Stop() error {
	s.mu.Lock()
	if s.attached {
		// Do not kill externally running server when simply docking Drakkar!
		s.mu.Unlock()
		log.Printf("[Drakkar Supervisor] Detached from running server process (PID %d)", s.pid)
		return nil
	}

	if s.cmd == nil || s.cmd.Process == nil || s.state != ProcessRunning {
		s.mu.Unlock()
		return nil
	}

	pid := s.pid
	s.state = ProcessStopped
	s.mu.Unlock()

	if runtime.GOOS == "windows" {
		// Use taskkill /T /F to kill child processes of cmd.exe (such as valheim_server.exe)
		killCmd := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
		_ = killCmd.Run()
	} else {
		_ = s.cmd.Process.Kill()
	}

	return nil
}

// Restart cleanly stops and relaunches the supervised server process
func (s *Supervisor) Restart(ctx context.Context) error {
	s.mu.Lock()
	if s.attached {
		s.mu.Unlock()
		return fmt.Errorf("cannot restart unmanaged attached process")
	}
	s.state = ProcessRestart
	s.mu.Unlock()

	_ = s.Stop()
	time.Sleep(1 * time.Second)

	return s.Start(ctx)
}

// GetInfo returns current status snapshot of the process
func (s *Supervisor) GetInfo() (state ProcessState, pid int, uptime string, path string, attached bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state = s.state
	pid = s.pid
	path = s.execPath
	attached = s.attached

	if s.state == ProcessRunning && !s.startTime.IsZero() {
		dur := time.Since(s.startTime).Truncate(time.Second)
		h := int(dur.Hours())
		m := int(dur.Minutes()) % 60
		sec := int(dur.Seconds()) % 60
		uptime = fmt.Sprintf("%02dh %02dm %02ds", h, m, sec)
	} else {
		uptime = "00h 00m 00s"
	}
	return
}

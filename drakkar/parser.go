package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	reJoinCode     = regexp.MustCompile(`(?i)(?:join code (\d+)|Join Code\s*[:=]?\s*(\d{5,8}))`)
	reVersion      = regexp.MustCompile(`(?i)(?:Valheim version:\s*([0-9\.]+)|Console:\s*Valheim\s*([0-9\.]+))`)
	reDay          = regexp.MustCompile(`(?i)(?:day:(\d+)|Day (\d+)|time\s*[:=]?\s*[\d\.]+\s*,\s*day\s*[:=]?\s*(\d+))`)
	reWorld        = regexp.MustCompile(`(?i)(?:ZNet\.LoadWorld:\s*([^\s\(]+)|Get create world\s*([^\r\n]+))`)
	rePlayerLogin  = regexp.MustCompile(`(?i)Got character ZDOID from (.+?)\s*:\s*(-?\d+:\d+)`)
	rePlayerLogout = regexp.MustCompile(`(?i)Destroying abandoned non persistent zdo\s+(-?\d+:\d+)`)
	reWorldSave    = regexp.MustCompile(`(?i)(?:World save \(\d+/\d+\) done|World saved \( ([\d\.]+)ms \)|Save World Thread Started)`)
	reSocketClosed = regexp.MustCompile(`(?i)(?:ZPlayFabSocket::Dispose\. State: CLOSED|RPC_Disconnect|Player connection lost|Closing socket (\d+)|Destroying player (\d+))`)
	reZeroPlayers  = regexp.MustCompile(`(?i)now 0 player\(s\)`)
)

// StateTracker manages in-memory game state and active player sessions
type StateTracker struct {
	mu            sync.RWMutex
	Server        ServerStatus
	ActivePlayers map[string]*PlayerSession // Key: player name
}

// NewStateTracker initializes a blank StateTracker
func NewStateTracker() *StateTracker {
	online := true
	zero := 0
	return &StateTracker{
		Server: ServerStatus{
			IsOnline:       &online,
			CurrentPlayers: &zero,
		},
		ActivePlayers: make(map[string]*PlayerSession),
	}
}

// ProcessLine inspects a raw Valheim log line and updates state accordingly, returning generated events
func (st *StateTracker) ProcessLine(line string, now time.Time) []ServerLogEvent {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	var events []ServerLogEvent
	ts := now.UTC().Format(time.RFC3339)

	// 1. Check PlayFab Join Code
	if match := reJoinCode.FindStringSubmatch(line); len(match) > 0 {
		code := match[1]
		if code == "" && len(match) > 2 {
			code = match[2]
		}
		if code != "" && code != st.Server.JoinCode {
			st.Server.JoinCode = code
			events = append(events, ServerLogEvent{
				ID:        fmt.Sprintf("ev-%d", now.UnixMilli()),
				Timestamp: ts,
				Category:  CategorySystem,
				Level:     LevelInfo,
				Message:   fmt.Sprintf("PlayFab Join Code: %s", code),
				Raw:       line,
			})
		}
	}

	// 2. Check Server Version
	if match := reVersion.FindStringSubmatch(line); len(match) > 0 {
		ver := match[1]
		if ver == "" && len(match) > 2 {
			ver = match[2]
		}
		if ver != "" {
			st.Server.Version = ver
		}
	}

	// 3. Check World Name
	if match := reWorld.FindStringSubmatch(line); len(match) > 0 {
		world := strings.TrimSpace(match[1])
		if world == "" && len(match) > 2 {
			world = strings.TrimSpace(match[2])
		}
		if world != "" {
			st.Server.WorldName = world
		}
	}

	// 4. Check Day Counter
	if match := reDay.FindStringSubmatch(line); len(match) > 0 {
		var dayStr string
		for i := 1; i < len(match); i++ {
			if match[i] != "" {
				dayStr = match[i]
				break
			}
		}
		if day, err := strconv.Atoi(dayStr); err == nil {
			if st.Server.DayCount == nil || *st.Server.DayCount != day {
				st.Server.DayCount = &day
				events = append(events, ServerLogEvent{
					ID:        fmt.Sprintf("ev-%d", now.UnixMilli()),
					Timestamp: ts,
					Category:  CategorySystem,
					Level:     LevelInfo,
					Message:   fmt.Sprintf("Day %d has dawned upon the realm.", day),
					Raw:       line,
				})
			}
		}
	}

	// 5. Check World Save
	if reWorldSave.MatchString(line) {
		st.Server.LastSavedAt = ts
		events = append(events, ServerLogEvent{
			ID:        fmt.Sprintf("ev-%d", now.UnixMilli()),
			Timestamp: ts,
			Category:  CategorySave,
			Level:     LevelSuccess,
			Message:   "World saved successfully.",
			Raw:       line,
		})
	}

	// 6. Check Player Login
	if match := rePlayerLogin.FindStringSubmatch(line); len(match) >= 3 {
		pName := strings.TrimSpace(match[1])
		zdoid := strings.TrimSpace(match[2])
		
		st.ActivePlayers[pName] = &PlayerSession{
			ID:             fmt.Sprintf("p-%s", pName),
			Name:           pName,
			CharacterZDOID: zdoid,
			ConnectedAt:    ts,
			IsOnline:       true,
		}

		st.updatePlayerCount()

		events = append(events, ServerLogEvent{
			ID:        fmt.Sprintf("ev-%d", now.UnixMilli()),
			Timestamp: ts,
			Category:  CategoryPlayer,
			Level:     LevelInfo,
			Message:   fmt.Sprintf("Player '%s' connected to the server.", pName),
			Raw:       line,
		})
	}

	// 7. Check Player Logout (by ZDOID)
	if match := rePlayerLogout.FindStringSubmatch(line); len(match) >= 2 {
		zdoid := strings.TrimSpace(match[1])
		for _, p := range st.ActivePlayers {
			if p.CharacterZDOID == zdoid && p.IsOnline {
				p.IsOnline = false
				p.DisconnectedAt = ts
				st.updatePlayerCount()

				events = append(events, ServerLogEvent{
					ID:        fmt.Sprintf("ev-%d", now.UnixMilli()),
					Timestamp: ts,
					Category:  CategoryPlayer,
					Level:     LevelInfo,
					Message:   fmt.Sprintf("Player '%s' left the realm.", p.Name),
					Raw:       line,
				})
				break
			}
		}
	}

	// 8. Sockets closed or zero players reset
	if reZeroPlayers.MatchString(line) || reSocketClosed.MatchString(line) {
		onlineCount := 0
		for _, p := range st.ActivePlayers {
			if p.IsOnline {
				onlineCount++
			}
		}
		// If 0 players or only 1 left on connection lost, mark remaining offline
		if reZeroPlayers.MatchString(line) || onlineCount <= 1 {
			for _, p := range st.ActivePlayers {
				if p.IsOnline {
					p.IsOnline = false
					p.DisconnectedAt = ts
				}
			}
			st.updatePlayerCount()
		}
	}

	return events
}

func (st *StateTracker) updatePlayerCount() {
	count := 0
	for _, p := range st.ActivePlayers {
		if p.IsOnline {
			count++
		}
	}
	st.Server.CurrentPlayers = &count
}

// GetSnapshot returns a clone of the current ServerStatus and all players
func (st *StateTracker) GetSnapshot() (ServerStatus, []PlayerSession) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	srvClone := st.Server
	players := make([]PlayerSession, 0, len(st.ActivePlayers))
	for _, p := range st.ActivePlayers {
		players = append(players, *p)
	}

	return srvClone, players
}

package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	reJoinCode     = regexp.MustCompile(`(?i)(?:join code (\d+)|Join Code\s*[:=]?\s*(\d{5,8}))`)
	reVersion      = regexp.MustCompile(`(?i)(?:Valheim version:\s*(?:[a-zA-Z]-)?([0-9\.]+)|Console:\s*Valheim\s*(?:[a-zA-Z]-)?([0-9\.]+))`)
	reDay          = regexp.MustCompile(`(?i)(?:day:(\d+)|Day (\d+)|time\s*[:=]?\s*[\d\.]+\s*,\s*day\s*[:=]?\s*(\d+))`)
	reWorld        = regexp.MustCompile(`(?i)(?:ZNet\.LoadWorld:\s*([^\s\(]+)|Get create world\s*([^\r\n]+))`)
	rePlayerLogin  = regexp.MustCompile(`(?i)Got character ZDOID from (.+?)\s*:\s*(-?\d+:\d+)`)
	rePlayerLogout = regexp.MustCompile(`(?i)Destroying abandoned non persistent zdo\s+(-?\d+:\d+)(?:\s+owner\s+(-?\d+))?`)
	reWorldSave    = regexp.MustCompile(`(?i)(?:World save \(\d+/\d+\) done|World saved \( ([\d\.]+)ms \)|Save World Thread Started)`)
	reSocketClosed = regexp.MustCompile(`(?i)(?:Closing socket\s+(-?\d+)|Destroying player\s+(-?\d+))`)
	rePlayerCount  = regexp.MustCompile(`(?i)now\s+(\d+)\s+player\(s\)`)
)

// StateTracker manages in-memory game state and active player sessions
type StateTracker struct {
	mu            sync.RWMutex
	Server        ServerStatus
	ActivePlayers map[string]*PlayerSession // Key: player name
	WorldOverride string
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

	// 6. Check Player Login / Death / Respawn
	if match := rePlayerLogin.FindStringSubmatch(line); len(match) >= 3 {
		pName := strings.TrimSpace(match[1])
		zdoid := strings.TrimSpace(match[2])
		peerID := ""
		if parts := strings.Split(zdoid, ":"); len(parts) > 0 && parts[0] != "0" {
			peerID = parts[0]
		}

		if zdoid == "0:0" {
			// Combat Death Event
			if existing, exists := st.ActivePlayers[pName]; exists {
				existing.CharacterZDOID = "0:0"
			} else {
				st.ActivePlayers[pName] = &PlayerSession{
					ID:             fmt.Sprintf("p-%s", pName),
					Name:           pName,
					CharacterZDOID: "0:0",
					PeerID:         peerID,
					ConnectedAt:    ts,
					IsOnline:       true,
				}
			}

			events = append(events, ServerLogEvent{
				ID:        fmt.Sprintf("ev-%d", now.UnixMilli()),
				Timestamp: ts,
				Category:  CategoryCombat,
				Level:     LevelWarn,
				Message:   fmt.Sprintf("Player '%s' has fallen in battle.", pName),
				Raw:       line,
			})
		} else {
			existing, exists := st.ActivePlayers[pName]
			wasOnline := exists && existing.IsOnline && existing.CharacterZDOID != "0:0"
			wasDead := exists && existing.CharacterZDOID == "0:0"

			connTime := ts
			if exists && (wasOnline || wasDead) && existing.ConnectedAt != "" {
				connTime = existing.ConnectedAt
			}

			st.ActivePlayers[pName] = &PlayerSession{
				ID:             fmt.Sprintf("p-%s", pName),
				Name:           pName,
				CharacterZDOID: zdoid,
				PeerID:         peerID,
				ConnectedAt:    connTime,
				IsOnline:       true,
			}

			st.updatePlayerCount()

			if wasDead {
				events = append(events, ServerLogEvent{
					ID:        fmt.Sprintf("ev-%d", now.UnixMilli()),
					Timestamp: ts,
					Category:  CategoryPlayer,
					Level:     LevelInfo,
					Message:   fmt.Sprintf("Player '%s' respawned.", pName),
					Raw:       line,
				})
			} else if !wasOnline {
				events = append(events, ServerLogEvent{
					ID:        fmt.Sprintf("ev-%d", now.UnixMilli()),
					Timestamp: ts,
					Category:  CategoryPlayer,
					Level:     LevelInfo,
					Message:   fmt.Sprintf("Player '%s' connected to the server.", pName),
					Raw:       line,
				})
			}
		}
	}

	// 7. Check Player Logout
	if match := rePlayerLogout.FindStringSubmatch(line); len(match) >= 2 {
		zdoid := strings.TrimSpace(match[1])
		owner := ""
		if len(match) > 2 {
			owner = strings.TrimSpace(match[2])
		}
		zdoidPrefix := strings.Split(zdoid, ":")[0]

		for _, p := range st.ActivePlayers {
			if !p.IsOnline {
				continue
			}
			matched := (p.CharacterZDOID != "" && p.CharacterZDOID == zdoid) ||
				(owner != "" && p.PeerID == owner) ||
				(p.PeerID != "" && zdoidPrefix != "" && p.PeerID == zdoidPrefix)

			if matched {
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

	// 8. Sockets closed / Player destroyed
	if match := reSocketClosed.FindStringSubmatch(line); len(match) >= 2 {
		targetID := strings.TrimSpace(match[1])
		if targetID != "" {
			for _, p := range st.ActivePlayers {
				if p.IsOnline && (p.PeerID == targetID || strings.HasPrefix(p.CharacterZDOID, targetID+":")) {
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
	}

	// 9. Reconcile with authoritative player count from server (e.g. "now 4 player(s)", "now 0 player(s)")
	if match := rePlayerCount.FindStringSubmatch(line); len(match) >= 2 {
		if c, err := strconv.Atoi(match[1]); err == nil {
			st.Server.CurrentPlayers = &c
			if c == 0 {
				for _, p := range st.ActivePlayers {
					if p.IsOnline {
						p.IsOnline = false
						p.DisconnectedAt = ts
					}
				}
			}
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

// GetSnapshot returns a clone of the current ServerStatus and all players sorted deterministically
func (st *StateTracker) GetSnapshot() (ServerStatus, []PlayerSession) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	srvClone := st.Server
	if st.WorldOverride != "" {
		srvClone.WorldName = st.WorldOverride
	}
	players := make([]PlayerSession, 0, len(st.ActivePlayers))
	for _, p := range st.ActivePlayers {
		players = append(players, *p)
	}

	// Sort deterministically: Online first, then by ConnectedAt ascending, then by Name
	sort.Slice(players, func(i, j int) bool {
		if players[i].IsOnline != players[j].IsOnline {
			return players[i].IsOnline
		}
		if players[i].ConnectedAt != players[j].ConnectedAt {
			return players[i].ConnectedAt < players[j].ConnectedAt
		}
		return players[i].Name < players[j].Name
	})

	return srvClone, players
}

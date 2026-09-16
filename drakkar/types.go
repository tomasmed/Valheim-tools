package main

// EventCategory defines event taxonomy matching ValheimDash
type EventCategory string

const (
	CategoryPlayer  EventCategory = "player"
	CategorySave    EventCategory = "save"
	CategorySystem  EventCategory = "system"
	CategoryCombat  EventCategory = "combat"
	CategoryWarning EventCategory = "warning"
)

// EventLevel defines severity level matching ValheimDash
type EventLevel string

const (
	LevelInfo    EventLevel = "info"
	LevelWarn    EventLevel = "warn"
	LevelError   EventLevel = "error"
	LevelSuccess EventLevel = "success"
)

// PlayerSession represents a player session in the server
type PlayerSession struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	CharacterZDOID string `json:"characterZdoId,omitempty"`
	ConnectedAt    string `json:"connectedAt"`
	DisconnectedAt string `json:"disconnectedAt,omitempty"`
	IsOnline       bool   `json:"isOnline"`
	SteamID        string `json:"steamId,omitempty"`
}

// ServerStatus contains mutable metadata about the Valheim server
type ServerStatus struct {
	IsOnline       *bool   `json:"isOnline,omitempty"`
	ServerName     string  `json:"serverName,omitempty"`
	WorldName      string  `json:"worldName,omitempty"`
	JoinCode       string  `json:"joinCode,omitempty"`
	Version        string  `json:"version,omitempty"`
	DayCount       *int    `json:"dayCount,omitempty"`
	CurrentPlayers *int    `json:"currentPlayers,omitempty"`
	LastSavedAt    string  `json:"lastSavedAt,omitempty"`
}

// ServerLogEvent represents a structured event emitted to the Mead Hall
type ServerLogEvent struct {
	ID        string        `json:"id"`
	Timestamp string        `json:"timestamp"`
	Category  EventCategory `json:"category"`
	Level     EventLevel    `json:"level"`
	Message   string        `json:"message"`
	Raw       string        `json:"raw,omitempty"`
}

// TelemetryPayload matches the POST /api/telemetry schema
type TelemetryPayload struct {
	Server    *ServerStatus    `json:"server,omitempty"`
	Players   []PlayerSession  `json:"players,omitempty"`
	Events    []ServerLogEvent `json:"events,omitempty"`
	Source    string           `json:"source,omitempty"`
	Timestamp string           `json:"timestamp,omitempty"`
}

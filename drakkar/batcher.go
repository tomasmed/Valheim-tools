package main

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

// BatcherStats provides a snapshot of batcher telemetry metrics
type BatcherStats struct {
	PendingEvents int
	BatchSize     int
	TotalBatches  int
	TotalEvents   int
	LastStatus    string
	LastFlush     time.Time
}

// Batcher debounces and batches log events before pushing to Mead Hall
type Batcher struct {
	client            *HTTPClient
	tracker           *StateTracker
	batchSize         int
	batchTime         time.Duration
	heartbeatInterval time.Duration

	mu            sync.Mutex
	eventsQueue   []ServerLogEvent
	lastFlush     time.Time
	forceFlushCh  chan struct{}
	eventCallback func(ServerLogEvent)
	totalBatches  int
	totalEvents   int
	lastStatus    string
}

// NewBatcher creates a new Batcher instance
func NewBatcher(client *HTTPClient, tracker *StateTracker, batchSize int, batchTimeSec int) *Batcher {
	if batchSize <= 0 {
		batchSize = 25
	}
	if batchTimeSec <= 0 {
		batchTimeSec = 3
	}

	return &Batcher{
		client:            client,
		tracker:           tracker,
		batchSize:         batchSize,
		batchTime:         time.Duration(batchTimeSec) * time.Second,
		heartbeatInterval: 45 * time.Second,
		eventsQueue:       make([]ServerLogEvent, 0, batchSize*2),
		lastFlush:         time.Now(),
		forceFlushCh:      make(chan struct{}, 1),
		lastStatus:        "INITIALIZED",
	}
}

// SetEventCallback registers a handler to be notified whenever new events are parsed
func (b *Batcher) SetEventCallback(cb func(ServerLogEvent)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.eventCallback = cb
}

// TriggerFlush signals an immediate flush without waiting for debounce timeout
func (b *Batcher) TriggerFlush() {
	select {
	case b.forceFlushCh <- struct{}{}:
	default:
	}
}

// GetStats returns telemetry batch metrics
func (b *Batcher) GetStats() BatcherStats {
	b.mu.Lock()
	defer b.mu.Unlock()

	return BatcherStats{
		PendingEvents: len(b.eventsQueue),
		BatchSize:     b.batchSize,
		TotalBatches:  b.totalBatches,
		TotalEvents:   b.totalEvents,
		LastStatus:    b.lastStatus,
		LastFlush:     b.lastFlush,
	}
}

// Run begins consuming parsed lines and triggers debounced or threshold flushes
func (b *Batcher) Run(ctx context.Context, linesCh <-chan string) {
	flushTicker := time.NewTicker(b.batchTime)
	defer flushTicker.Stop()

	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final flush on exit
			b.flush(context.Background())
			return

		case <-b.forceFlushCh:
			b.flush(ctx)

		case line, ok := <-linesCh:
			if !ok {
				b.flush(ctx)
				return
			}

			events := b.tracker.ProcessLine(line, time.Now())
			if len(events) > 0 {
				b.mu.Lock()
				cb := b.eventCallback
				b.eventsQueue = append(b.eventsQueue, events...)
				shouldFlush := len(b.eventsQueue) >= b.batchSize
				b.mu.Unlock()

				if cb != nil {
					for _, ev := range events {
						cb(ev)
					}
				}

				if shouldFlush {
					b.flush(ctx)
				}
			}

		case <-flushTicker.C:
			b.mu.Lock()
			hasPending := len(b.eventsQueue) > 0
			b.mu.Unlock()

			if hasPending {
				b.flush(ctx)
			}

		case <-heartbeatTicker.C:
			b.mu.Lock()
			timeSinceFlush := time.Since(b.lastFlush)
			b.mu.Unlock()

			if timeSinceFlush >= b.heartbeatInterval {
				b.sendHeartbeat(ctx)
			}
		}
	}
}

// flush sends accumulated events and the current server/player snapshot
func (b *Batcher) flush(ctx context.Context) {
	b.mu.Lock()
	if len(b.eventsQueue) == 0 {
		b.mu.Unlock()
		return
	}

	eventsToSend := b.eventsQueue
	b.eventsQueue = make([]ServerLogEvent, 0, b.batchSize*2)
	b.lastFlush = time.Now()
	b.mu.Unlock()

	server, players := b.tracker.GetSnapshot()

	if b.client == nil {
		b.mu.Lock()
		b.totalBatches++
		b.totalEvents += len(eventsToSend)
		b.lastStatus = "STANDALONE (NO SYNC)"
		b.mu.Unlock()
		return
	}

	payload := TelemetryPayload{
		Server:  &server,
		Players: players,
		Events:  eventsToSend,
	}

	if err := b.client.SendTelemetry(ctx, payload); err != nil {
		b.mu.Lock()
		b.lastStatus = formatErrorStatus(err)
		b.mu.Unlock()
		log.Printf("[Drakkar] Failed to flush telemetry batch: %v", err)
	} else {
		b.mu.Lock()
		b.totalBatches++
		b.totalEvents += len(eventsToSend)
		b.lastStatus = "HTTP 200 OK"
		b.mu.Unlock()
		log.Printf("[Drakkar] Flushed batch: %d event(s) synced with Mead Hall (players: %d)", len(eventsToSend), len(players))
	}
}

// sendHeartbeat sends a periodic keepalive payload
func (b *Batcher) sendHeartbeat(ctx context.Context) {
	b.mu.Lock()
	b.lastFlush = time.Now()
	b.mu.Unlock()

	server, players := b.tracker.GetSnapshot()

	if b.client == nil {
		b.mu.Lock()
		b.lastStatus = "STANDALONE (NO SYNC)"
		b.mu.Unlock()
		return
	}

	payload := TelemetryPayload{
		Server:  &server,
		Players: players,
	}

	if err := b.client.SendTelemetry(ctx, payload); err != nil {
		b.mu.Lock()
		b.lastStatus = formatErrorStatus(err)
		b.mu.Unlock()
		log.Printf("[Drakkar] Heartbeat failed: %v", err)
	} else {
		b.mu.Lock()
		b.lastStatus = "HEARTBEAT OK"
		b.mu.Unlock()
		log.Printf("[Drakkar] Heartbeat sent to Mead Hall (server online, players: %d)", len(players))
	}
}

// formatErrorStatus converts errors into concise, human-readable status codes for the HUD
func formatErrorStatus(err error) string {
	if err == nil {
		return "HTTP 200 OK"
	}
	errStr := err.Error()
	switch {
	case strings.Contains(errStr, "401"):
		return "401 UNAUTHORIZED"
	case strings.Contains(errStr, "403"):
		return "403 FORBIDDEN"
	case strings.Contains(errStr, "refused") || strings.Contains(errStr, "connectex"):
		return "DASHBOARD OFFLINE"
	case strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline"):
		return "TIMEOUT"
	case strings.Contains(errStr, "404"):
		return "404 NOT FOUND"
	default:
		if len(errStr) > 24 {
			return "ERR: " + errStr[:21] + "..."
		}
		return "ERR: " + errStr
	}
}

package main

import (
	"context"
	"log"
	"sync"
	"time"
)

// Batcher debounces and batches log events before pushing to Mead Hall
type Batcher struct {
	client            *HTTPClient
	tracker           *StateTracker
	batchSize         int
	batchTime         time.Duration
	heartbeatInterval time.Duration

	mu          sync.Mutex
	eventsQueue []ServerLogEvent
	lastFlush   time.Time
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

		case line, ok := <-linesCh:
			if !ok {
				b.flush(ctx)
				return
			}

			events := b.tracker.ProcessLine(line, time.Now())
			if len(events) > 0 {
				b.mu.Lock()
				b.eventsQueue = append(b.eventsQueue, events...)
				shouldFlush := len(b.eventsQueue) >= b.batchSize
				b.mu.Unlock()

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

	payload := TelemetryPayload{
		Server:  &server,
		Players: players,
		Events:  eventsToSend,
	}

	if err := b.client.SendTelemetry(ctx, payload); err != nil {
		log.Printf("[Drakkar] Failed to flush telemetry batch: %v", err)
	} else {
		log.Printf("[Drakkar] Flushed batch: %d event(s) synced with Mead Hall (players: %d)", len(eventsToSend), len(players))
	}
}

// sendHeartbeat sends a periodic keepalive payload
func (b *Batcher) sendHeartbeat(ctx context.Context) {
	b.mu.Lock()
	b.lastFlush = time.Now()
	b.mu.Unlock()

	server, players := b.tracker.GetSnapshot()

	payload := TelemetryPayload{
		Server:  &server,
		Players: players,
	}

	if err := b.client.SendTelemetry(ctx, payload); err != nil {
		log.Printf("[Drakkar] Heartbeat failed: %v", err)
	} else {
		log.Printf("[Drakkar] Heartbeat sent to Mead Hall (server online, players: %d)", len(players))
	}
}

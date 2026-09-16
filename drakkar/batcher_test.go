package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestBatcher_ThresholdFlush(t *testing.T) {
	var mu sync.Mutex
	var receivedPayloads []TelemetryPayload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret123" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var p TelemetryPayload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		receivedPayloads = append(receivedPayloads, p)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "secret123")
	tracker := NewStateTracker()
	batcher := NewBatcher(client, tracker, 5, 10) // batchSize: 5, batchTime: 10s

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	linesCh := make(chan string, 20)
	go batcher.Run(ctx, linesCh)

	// Send 5 lines that generate events
	for i := 1; i <= 5; i++ {
		linesCh <- fmt.Sprintf("09/12/2026 12:00:%02d: Got character ZDOID from Viking%d : 1000%d:1", i, i, i)
	}

	// Should flush almost immediately because threshold is 5
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	count := len(receivedPayloads)
	mu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 flushed batch, got %d", count)
	}

	mu.Lock()
	eventsCount := len(receivedPayloads[0].Events)
	mu.Unlock()

	if eventsCount != 5 {
		t.Errorf("expected 5 events in batch, got %d", eventsCount)
	}
}

func TestBatcher_TimerFlush(t *testing.T) {
	var mu sync.Mutex
	var receivedPayloads []TelemetryPayload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p TelemetryPayload
		_ = json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		receivedPayloads = append(receivedPayloads, p)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "")
	tracker := NewStateTracker()
	batcher := NewBatcher(client, tracker, 50, 1) // batchSize: 50, batchTime: 1s

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	linesCh := make(chan string, 10)
	go batcher.Run(ctx, linesCh)

	// Send 1 line (below threshold)
	linesCh <- "09/12/2026 12:00:00: World saved ( 15.0ms )"

	// Wait 1.5s for timer to trigger flush
	time.Sleep(1500 * time.Millisecond)

	mu.Lock()
	count := len(receivedPayloads)
	mu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 batch flush after timer, got %d", count)
	}

	mu.Lock()
	p := receivedPayloads[0]
	mu.Unlock()

	if len(p.Events) != 1 {
		t.Errorf("expected 1 event, got %d", len(p.Events))
	}
	if p.Events[0].Category != CategorySave {
		t.Errorf("expected save category, got %s", p.Events[0].Category)
	}
}

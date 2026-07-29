package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The gate exists to stop clock-aligned frames from all rendering at once, so
// the property that matters is the ceiling on simultaneous holders.
func TestRenderGateBoundsConcurrency(t *testing.T) {
	const limit = 2
	const callers = 8

	gate := NewRenderGate(limit)
	var inFlight, peak atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := gate.Acquire(context.Background(), "test")
			if err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			defer release()

			now := inFlight.Add(1)
			for {
				old := peak.Load()
				if now <= old || peak.CompareAndSwap(old, now) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			inFlight.Add(-1)
		}()
	}
	wg.Wait()

	if got := peak.Load(); got > limit {
		t.Fatalf("peak concurrency %d exceeds limit %d", got, limit)
	}
	if inFlight.Load() != 0 {
		t.Fatalf("slots leaked: %d still in flight", inFlight.Load())
	}
}

// A frame that hangs up mid-queue must not hold a slot hostage.
func TestRenderGateReleasesOnContextCancel(t *testing.T) {
	gate := NewRenderGate(1)

	release, err := gate.Acquire(context.Background(), "holder")
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	waiterDone := make(chan error, 1)
	go func() {
		_, err := gate.Acquire(ctx, "waiter")
		waiterDone <- err
	}()

	// Give the waiter a moment to actually block, then hang up on it.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-waiterDone:
		if err == nil {
			t.Fatal("cancelled waiter acquired a slot")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled waiter never returned")
	}

	release()

	// The slot the holder gave back must be usable.
	release2, err := gate.Acquire(context.Background(), "next")
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	release2()
}

// Release is deferred on paths that can also return early; a double call must
// not hand out a slot that was never taken.
func TestRenderGateReleaseIsIdempotent(t *testing.T) {
	gate := NewRenderGate(1)

	release, err := gate.Acquire(context.Background(), "once")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	release()
	release()

	// Capacity is still exactly one: take it, and a second Acquire must block.
	held, err := gate.Acquire(context.Background(), "holder")
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	defer held()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := gate.Acquire(ctx, "should block"); err == nil {
		t.Fatal("gate handed out more slots than its capacity")
	}
}

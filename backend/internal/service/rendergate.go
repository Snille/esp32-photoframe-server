package service

import (
	"context"
	"errors"
	"log"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

// Frames that share a rotation interval and have "align rotation to clock
// boundaries" enabled all wake on the same clock tick, so a 15-minute frame and
// two 30-minute frames collide every half hour — by design, not by chance. Each
// pull then runs a full pipeline (source fetch, headless-Chrome overlay render,
// dithering) concurrently on the same box, and the resulting spike is what makes
// a transfer stall or get cut off mid-image.
//
// The gate bounds how many of those pipelines run at once. Frames that arrive
// while the gate is full simply wait for their turn: the firmware allows 120 s
// per request and retries three times, so a short queue is invisible to them,
// while an overloaded server is not.
//
// This is damage control, not the cure — see the per-device rotation offset
// (rotate_offset), which stops the frames from arriving together at all.
const defaultRenderConcurrency = 2

// Hard cap on queue time. Well inside the firmware's 120 s request timeout, so a
// frame that gives up here still has retries left rather than timing out.
const renderQueueTimeout = 75 * time.Second

// ErrRenderBusy is returned when a slot did not become free in time. Callers
// should surface it as a retryable 503 rather than a hard failure.
var ErrRenderBusy = errors.New("image pipeline busy")

// RenderGate limits concurrent image-pipeline runs.
type RenderGate struct {
	slots   chan struct{}
	waiting atomic.Int32
}

func NewRenderGate(concurrency int) *RenderGate {
	if concurrency < 1 {
		concurrency = 1
	}
	return &RenderGate{slots: make(chan struct{}, concurrency)}
}

// Acquire takes a slot, waiting until one is free, the caller's context is done
// (e.g. the frame hung up), or renderQueueTimeout elapses. The returned release
// func is safe to call exactly once, typically deferred.
func (g *RenderGate) Acquire(ctx context.Context, label string) (func(), error) {
	// Fast path: a slot is free right now, no logging, no timer.
	select {
	case g.slots <- struct{}{}:
		return g.releaser(), nil
	default:
	}

	queued := g.waiting.Add(1)
	defer g.waiting.Add(-1)
	start := time.Now()
	log.Printf("Image pipeline busy, queuing %s (%d waiting)", label, queued)

	timer := time.NewTimer(renderQueueTimeout)
	defer timer.Stop()

	select {
	case g.slots <- struct{}{}:
		log.Printf("Image pipeline slot acquired for %s after %s", label, time.Since(start).Round(time.Millisecond))
		return g.releaser(), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, ErrRenderBusy
	}
}

func (g *RenderGate) releaser() func() {
	var once atomic.Bool
	return func() {
		if once.CompareAndSwap(false, true) {
			<-g.slots
		}
	}
}

// The pipeline competes for process-global resources (CPU for dithering, the
// single shared headless-Chrome instance), so the gate is process-global too:
// pull, push and preview all draw from the same pool.
var globalRenderGate = NewRenderGate(renderConcurrencyFromEnv())

// AcquireRenderSlot reserves capacity for one image-pipeline run.
func AcquireRenderSlot(ctx context.Context, label string) (func(), error) {
	return globalRenderGate.Acquire(ctx, label)
}

func renderConcurrencyFromEnv() int {
	raw := os.Getenv("MAX_CONCURRENT_RENDERS")
	if raw == "" {
		return defaultRenderConcurrency
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		log.Printf("Invalid MAX_CONCURRENT_RENDERS=%q, using %d", raw, defaultRenderConcurrency)
		return defaultRenderConcurrency
	}
	log.Printf("Image pipeline concurrency set to %d", n)
	return n
}

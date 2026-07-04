// Package safego guards background work against the Go runtime's default
// behavior: an unrecovered panic in ANY goroutine — even a detached
// fire-and-forget one — terminates the entire process, not just that
// goroutine. Echo's Recover() middleware only covers HTTP request handlers;
// it does nothing for goroutines started with a bare `go func() {}()`. This
// server has several of those (MQTT tickers, Immich/Synology auto-sync
// loops, async thumbnail rendering) running for the lifetime of the process,
// so a single transient bug in one of them would otherwise take down frame
// serving entirely until someone noticed and restarted the container.
package safego

import (
	"log"
	"runtime/debug"
)

// Go runs fn in a new goroutine, recovering any panic so it's logged instead
// of crashing the process.
func Go(name string, fn func()) {
	go func() {
		Safe(name, fn)
	}()
}

// Safe runs fn in the current goroutine, recovering any panic so it's logged
// instead of crashing the process. Use this (instead of Go) inside a
// long-lived loop, so the loop survives a panic in one iteration rather than
// exiting after the first one.
func Safe(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic in %s: %v\n%s", name, r, debug.Stack())
		}
	}()
	fn()
}

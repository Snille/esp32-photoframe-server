// Package version exposes the server's build version.
package version

// Version is the server version string. It is injected at build time via
//
//	-ldflags "-X github.com/aitjcize/esp32-photoframe-server/backend/internal/version.Version=<v>"
//
// (the Dockerfile derives <v> from config.yaml, the single source of truth).
// It defaults to "dev" for local / un-stamped builds.
var Version = "dev"

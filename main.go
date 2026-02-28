// Jotes is a note-browsing web application that renders files directly from a
// single configured root directory.
package main

import (
	"embed"
	"log"

	"jotes/config"
	"jotes/server"
)

// embeddedFS contains the bundled templates and static assets that Jotes serves.
//
//go:embed templates static
var embeddedFS embed.FS

// main loads runtime configuration, injects the embedded asset filesystem into
// the server package, and blocks while the Jotes HTTP server runs.
//
// Parameters:
//   - none: main reads process flags and environment variables indirectly via config.Load.
//
// Returns:
//   - none: fatal configuration or server errors terminate the process via log.Fatalf.
func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	server.SetStaticFS(embeddedFS)

	if err := server.Run(cfg, embeddedFS); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// Package config handles runtime configuration for the Jotes server.
// Command-line flags take precedence over environment variables.
package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config stores every runtime option that Jotes accepts.
type Config struct {
	// Host is the interface or hostname that the HTTP server binds to.
	Host string
	// Port is the TCP port that the Jotes HTTP server listens on.
	Port int
	// RootDir is the single filesystem directory that Jotes exposes as "/".
	RootDir string
	// DataDir is the directory that stores Jotes runtime data such as jotes.db.
	DataDir string
}

// Load parses command-line flags and environment variables, validates the
// resulting values, and returns the final configuration.
//
// Parameters:
//   - none: Load reads from the process-wide flag set and environment.
//
// Returns:
//   - *Config: the validated server configuration.
//   - error: non-nil when the host, port, root directory, or data directory is missing or invalid.
func Load() (*Config, error) {
	hostFlag := flag.String("host", "", "HTTP host to bind (env: JOTES_HOST, default: 0.0.0.0)")
	portFlag := flag.Int("port", 0, "HTTP port to listen on (env: JOTES_PORT, default: 7887)")
	dirFlag := flag.String("dir", "", "Root notes directory to expose as / (env: JOTES_DIR)")
	dataDirFlag := flag.String("data-dir", "", "Directory for Jotes runtime data such as jotes.db (env: JOTES_DATA_DIR, default: /etc/jotes)")
	flag.Parse()

	host := strings.TrimSpace(*hostFlag)
	if host == "" {
		host = strings.TrimSpace(os.Getenv("JOTES_HOST"))
		if host == "" {
			host = "0.0.0.0"
		}
	}

	port := *portFlag
	if port == 0 {
		if v := strings.TrimSpace(os.Getenv("JOTES_PORT")); v != "" {
			parsedPort, err := strconv.Atoi(v)
			if err != nil || parsedPort < 1 || parsedPort > 65535 {
				return nil, fmt.Errorf("invalid JOTES_PORT value %q", v)
			}
			port = parsedPort
		} else {
			port = 7887
		}
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid --port %d: must be between 1 and 65535", port)
	}

	rootDir := strings.TrimSpace(*dirFlag)
	args := flag.Args()
	if rootDir == "" {
		switch len(args) {
		case 0:
			rootDir = strings.TrimSpace(os.Getenv("JOTES_DIR"))
		case 1:
			rootDir = strings.TrimSpace(args[0])
		default:
			return nil, fmt.Errorf("only one root directory is supported; received %d positional paths", len(args))
		}
	} else if len(args) > 0 {
		return nil, fmt.Errorf("unexpected positional arguments: use only --dir for the root directory")
	}

	if rootDir == "" {
		return nil, fmt.Errorf("a root directory must be provided via --dir, JOTES_DIR, or one positional argument")
	}

	absRootDir, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve root directory %q: %w", rootDir, err)
	}

	info, err := os.Stat(absRootDir)
	if err != nil {
		return nil, fmt.Errorf("root directory %q: %w", absRootDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%q is not a directory", absRootDir)
	}

	resolvedRootDir, err := filepath.EvalSymlinks(absRootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve root directory symlinks for %q: %w", absRootDir, err)
	}

	dataDir := strings.TrimSpace(*dataDirFlag)
	if dataDir == "" {
		dataDir = strings.TrimSpace(os.Getenv("JOTES_DATA_DIR"))
		if dataDir == "" {
			dataDir = "/etc/jotes"
		}
	}

	resolvedDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve data directory %q: %w", dataDir, err)
	}

	return &Config{
		Host:    host,
		Port:    port,
		RootDir: resolvedRootDir,
		DataDir: resolvedDataDir,
	}, nil
}

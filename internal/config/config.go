package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"file-cleaner/internal/filesystem"
)

// Config is the runtime configuration of the file cleaner service.
type Config struct {
	// Addr is the HTTP listen address, e.g. ":6888".
	Addr string
	// Root is the only directory tree the service is allowed to touch.
	Root string
	// TrashMode moves deleted entries into TrashDir instead of unlinking them.
	TrashMode bool
	// MaxResultItems caps how many scan results are held in memory.
	MaxResultItems int
}

// Load reads configuration from the environment, applying safe defaults.
func Load() Config {
	c := Config{
		Addr:           env("FILE_CLEANER_ADDR", ":6888"),
		Root:           env("FILE_CLEANER_ROOT", "/mnt"),
		TrashMode:      envBool("FILE_CLEANER_TRASH", false),
		MaxResultItems: envInt("FILE_CLEANER_MAX_RESULTS", 500000),
	}
	c.Root = filesystem.CleanRoot(c.Root)
	return c
}

// TrashDir is where trashed entries are parked when TrashMode is enabled.
func (c Config) TrashDir() string {
	return filepath.Join(c.Root, ".file-cleaner-trash")
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

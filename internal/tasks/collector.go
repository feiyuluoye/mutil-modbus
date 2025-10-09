package tasks

import (
	"context"

	"modbus-simulator/internal/collector"
)

// Options defines initialization overrides for the collector.
// Mirrors the CLI flags used in cmd/collector/main.go.
type Options struct {
	ConfigPath     string
	StorageEnabled bool
	StorageDir     string
	StorageQueue   int
}

// InitAndRunCollector loads config, applies overrides, constructs the manager and runs it.
func InitAndRunCollector(ctx context.Context, opts Options) error {
	cfg, err := collector.LoadYAML(opts.ConfigPath)
	if err != nil {
		return err
	}

	// Override YAML with provided options
	if opts.StorageEnabled {
		cfg.System.Storage.Enabled = true
	}
	if opts.StorageDir != "" {
		cfg.System.Storage.DBPath = opts.StorageDir
		cfg.System.Storage.Enabled = true
	}
	if opts.StorageQueue > 0 {
		cfg.System.Storage.MaxQueueSize = opts.StorageQueue
		cfg.System.Storage.Enabled = true
	}

	mgr := &collector.Manager{Cfg: cfg}
	return mgr.Run(ctx)
}

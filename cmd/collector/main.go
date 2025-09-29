package main

import (
    "context"
    "flag"
    "log"
    "os"
    "os/signal"
    "syscall"

    "modbus-simulator/internal/collector"
)

func main() {
    var cfgPath string
    var storageEnabled bool
    var storageDir string
    var storageQueue int
    flag.StringVar(&cfgPath, "config", "config/config.yaml", "path to YAML config")
    flag.BoolVar(&storageEnabled, "storage-enabled", false, "enable JSONL/CSV storage output (overrides YAML)")
    flag.StringVar(&storageDir, "storage-dir", "", "storage output directory (overrides YAML system.storage.db_path)")
    flag.IntVar(&storageQueue, "storage-queue", 0, "storage queue size (overrides YAML system.storage.max_queue_size)")
    flag.Parse()

    cfg, err := collector.LoadYAML(cfgPath)
    if err != nil {
        log.Fatalf("load yaml config: %v", err)
    }

    // Override YAML with CLI flags for storage
    if storageEnabled {
        cfg.System.Storage.Enabled = true
    }
    if storageDir != "" {
        cfg.System.Storage.DBPath = storageDir
        cfg.System.Storage.Enabled = true
    }
    if storageQueue > 0 {
        cfg.System.Storage.MaxQueueSize = storageQueue
        cfg.System.Storage.Enabled = true
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Handle SIGINT/SIGTERM for graceful shutdown
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-sigCh
		log.Printf("received signal: %v, shutting down...", s)
		cancel()
	}()

	mgr := &collector.Manager{Cfg: cfg}
	if err := mgr.Run(ctx); err != nil {
		log.Printf("manager exited with error: %v", err)
	}
}

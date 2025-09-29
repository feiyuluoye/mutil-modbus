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
	flag.StringVar(&cfgPath, "config", "config/config.yaml", "path to YAML config")
	flag.Parse()

	cfg, err := collector.LoadYAML(cfgPath)
	if err != nil {
		log.Fatalf("load yaml config: %v", err)
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

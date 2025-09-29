package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	collector "modbus-simulator/internal/collector"
	servermgr "modbus-simulator/internal/servermgr"
	"modbus-simulator/internal/output"
)

func main() {
	var cfgPath string
	var outJSON string
	var outCSV string
	var wait string
	flag.StringVar(&cfgPath, "config", "config/config.yaml", "path to YAML config")
	flag.StringVar(&outJSON, "json", "", "path to write JSON snapshot (optional)")
	flag.StringVar(&outCSV, "csv", "", "path to write CSV snapshot (optional)")
	flag.StringVar(&wait, "wait", "2s", "wait duration before snapshot (e.g., 2s)")
	flag.Parse()

	if outJSON == "" && outCSV == "" {
		log.Fatalf("no output specified: set --json and/or --csv")
	}

	cfg, err := collector.LoadYAML(cfgPath)
	if err != nil {
		log.Fatalf("load yaml config: %v", err)
	}

	mgr := servermgr.NewManager(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sigs; cancel() }()

	// start servers in background
	go func() {
		if err := mgr.Run(ctx); err != nil {
			log.Printf("server manager exited with error: %v", err)
		}
	}()

	// wait a bit so the manager can apply first CSV row
	d, err := time.ParseDuration(wait)
	if err != nil { d = 2 * time.Second }
	time.Sleep(d)

	// take snapshot
	snaps, err := mgr.Snapshot()
	if err != nil {
		log.Fatalf("snapshot error: %v", err)
	}

	if outJSON != "" {
		if err := output.WriteJSON(outJSON, snaps); err != nil {
			log.Printf("write json error: %v", err)
		}
	}
	if outCSV != "" {
		if err := output.WriteCSV(outCSV, snaps); err != nil {
			log.Printf("write csv error: %v", err)
		}
	}

	// done
	cancel()
}

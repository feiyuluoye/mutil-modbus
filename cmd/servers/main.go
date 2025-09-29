package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"time"
	"syscall"

	collector "modbus-simulator/internal/collector"
	"modbus-simulator/internal/output"
	servermgr "modbus-simulator/internal/servermgr"
)

func main() {
	var cfgPath string
	var snapJSON string
	var snapCSV string
	var snapWait string
	flag.StringVar(&cfgPath, "config", "config/config.yaml", "path to YAML config for servers")
	flag.StringVar(&snapJSON, "snapshot-json", "", "optional path to write a one-time JSON snapshot")
	flag.StringVar(&snapCSV, "snapshot-csv", "", "optional path to write a one-time CSV snapshot")
	flag.StringVar(&snapWait, "snapshot-wait", "3s", "wait duration before taking snapshot (e.g., 3s)")
	flag.Parse()

	rootCfg, err := collector.LoadYAML(cfgPath)
	if err != nil {
		log.Fatalf("load yaml config %s: %v", cfgPath, err)
	}

	mgr := servermgr.NewManager(rootCfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		log.Printf("shutting down servers...")
		cancel()
	}()

	// If snapshot flags are set, run servers, wait, take snapshot, export, and exit.
	if snapJSON != "" || snapCSV != "" {
		// start servers in background
		done := make(chan struct{})
		go func() {
			if err := mgr.Run(ctx); err != nil {
				log.Printf("server manager exited with error: %v", err)
			}
			close(done)
		}()

		// wait for servers to start and first CSV write to apply
		d, err := time.ParseDuration(snapWait)
		if err != nil {
			d = 3 * time.Second
		}
		time.Sleep(d)

		snaps, err := mgr.Snapshot()
		if err != nil {
			log.Fatalf("snapshot error: %v", err)
		}
		if snapJSON != "" {
			if err := output.WriteJSON(snapJSON, snaps); err != nil {
				log.Fatalf("write snapshot json: %v", err)
			}
		}
		if snapCSV != "" {
			if err := output.WriteCSV(snapCSV, snaps); err != nil {
				log.Fatalf("write snapshot csv: %v", err)
			}
		}
		// stop and exit
		cancel()
		<-done
		return
	}

	// Default behavior: run servers until interrupted.
	if err := mgr.Run(ctx); err != nil {
		log.Printf("server manager exited with error: %v", err)
	}
}

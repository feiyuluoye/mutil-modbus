package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	collector "modbus-simulator/internal/collector"
	servermgr "modbus-simulator/internal/servermgr"
)

func main() {
	var cfgPath string
	flag.StringVar(&cfgPath, "config", "config/config.yaml", "path to YAML config for servers")
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

	if err := mgr.Run(ctx); err != nil {
		log.Printf("server manager exited with error: %v", err)
	}
}

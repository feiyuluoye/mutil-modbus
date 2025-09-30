package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"time"

	dbpkg "modbus-simulator/internal/db"
)

func main() {
	var (
		dbPath   = flag.String("db", "./data.sqlite", "path to sqlite database file")
		deviceID = flag.String("device", "", "target device_id to analyze (required)")
		limit    = flag.Int("limit", 0, "max number of device points to return (0 = no limit)")
	)
	flag.Parse()

	if *deviceID == "" {
		log.Fatal("-device is required")
	}

	db, err := dbpkg.Open(*dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var raw []byte
	if *limit > 0 {
		raw, err = db.StatsJSONWithLimit(ctx, *deviceID, *limit)
	} else {
		raw, err = db.StatsJSON(ctx, *deviceID)
	}
	if err != nil {
		log.Fatalf("stats: %v", err)
	}

	// pretty print
	var pretty map[string]any
	if err := json.Unmarshal(raw, &pretty); err == nil {
		b, _ := json.MarshalIndent(pretty, "", "  ")
		fmt.Println(string(b))
		return
	}
	fmt.Println(string(raw))
}

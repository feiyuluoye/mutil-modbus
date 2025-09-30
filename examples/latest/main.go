package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	dbpkg "modbus-simulator/internal/db"
)

func main() {
	var (
		dbPath string
		pretty bool
		timeout time.Duration
		serverID string
		deviceID string
	)
	flag.StringVar(&dbPath, "db", "./data.sqlite", "path to sqlite database file")
	flag.BoolVar(&pretty, "pretty", true, "pretty-print JSON output")
	flag.DurationVar(&timeout, "timeout", 5*time.Second, "context timeout for DB query")
	flag.StringVar(&serverID, "server", "", "optional server_id filter")
	flag.StringVar(&deviceID, "device", "", "optional device_id filter")
	flag.Parse()

	db, err := dbpkg.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open db: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	pts, err := db.LatestPoints(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "query latest points: %v\n", err)
		os.Exit(1)
	}

	// Apply optional filters client-side
	if serverID != "" || deviceID != "" {
		filtered := make([]dbpkg.PointLatest, 0, len(pts))
		for _, p := range pts {
			if serverID != "" && p.ServerID != serverID {
				continue
			}
			if deviceID != "" && p.DeviceID != deviceID {
				continue
			}
			filtered = append(filtered, p)
		}
		pts = filtered
	}

	var out []byte
	if pretty {
		out, err = json.MarshalIndent(pts, "", "  ")
	} else {
		out, err = json.Marshal(pts)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal json: %v\n", err)
		os.Exit(1)
	}
	os.Stdout.Write(out)
	os.Stdout.Write([]byte{'\n'})
}

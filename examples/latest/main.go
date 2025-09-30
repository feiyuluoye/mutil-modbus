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
    dbPath := flag.String("db", "./data.sqlite", "path to sqlite database file")
    pretty := flag.Bool("pretty", true, "print formatted JSON output")
    flag.Parse()

    db, err := dbpkg.Open(*dbPath)
    if err != nil {
        log.Fatalf("open db: %v", err)
    }
    defer db.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    raw, err := db.LatestPointsJSON(ctx)
    if err != nil {
        log.Fatalf("latest points: %v", err)
    }

    if !*pretty {
        fmt.Println(string(raw))
        return
    }

    var points []map[string]any
    if err := json.Unmarshal(raw, &points); err != nil {
        log.Printf("warn: pretty print decode failed: %v", err)
        fmt.Println(string(raw))
        return
    }
    buf, err := json.MarshalIndent(points, "", "  ")
    if err != nil {
        log.Printf("warn: pretty print encode failed: %v", err)
        fmt.Println(string(raw))
        return
    }
    fmt.Println(string(buf))
}

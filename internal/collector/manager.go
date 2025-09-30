package collector

import (
    "context"
    "log"
    "strings"
    "sync"
    "time"

    dbpkg "modbus-simulator/internal/db"
    utils "modbus-simulator/internal/utils"
)

// Manager coordinates running multiple device collectors concurrently.

type Manager struct {
    Cfg     RootConfig
    OnValue ResultHandler // optional global handler
}

func (m *Manager) Run(ctx context.Context) error {
    // optional storage
    var store *Storage
    var storeClose func()
    if m.Cfg.System.Storage.Enabled {
        ft := strings.ToLower(strings.TrimSpace(m.Cfg.System.Storage.FileType))
        switch ft {
        case "", "csv", "json", "jsonl", "json+csv", "csv+json", "both", "all",
            "db", "json+db", "db+json", "csv+db", "db+csv":
            s, err := NewStorage(
                m.Cfg.System.Storage.DBPath,
                ft,
                m.Cfg.System.Storage.MaxWorkers,
                m.Cfg.System.Storage.MaxQueueSize,
            )
            if err != nil {
                log.Printf("storage init failed: %v (continuing without storage)", err)
            } else {
                store = s
                storeClose = func() { store.Close() }
                // If DB is enabled and empty, initialize schema data from config
                if store.enableDB && store.db != nil {
                    if err := m.initDatabaseFromConfig(store.db); err != nil {
                        log.Printf("database init failed: %v", err)
                    }
                }
                storeHandler := store.Handle
                // TTL cache to avoid writing unchanged values; use near-equal float compare
                ttl := m.Cfg.System.Storage.CacheTTL
                vc := utils.NewValueCache(ttl)
                if m.OnValue == nil {
                    m.OnValue = func(v PointValue) error {
                        key := v.DeviceID + "|" + v.PointName + "|" + v.Register + "|" + v.ServerID
                        if old, ok := vc.GetValue(key); ok && utils.FloatsEqual(old, v.Value) {
                            return nil
                        }
                        if err := storeHandler(v); err != nil {
                            return err
                        }
                        vc.SetValue(key, v.Value)
                        return nil
                    }
                } else {
                    userH := m.OnValue
                    m.OnValue = func(v PointValue) error {
                        key := v.DeviceID + "|" + v.PointName + "|" + v.Register + "|" + v.ServerID
                        if old, ok := vc.GetValue(key); ok && utils.FloatsEqual(old, v.Value) {
                            return nil
                        }
                        if err := userH(v); err != nil {
                            log.Printf("custom handler error: %v", err)
                        }
                        if err := storeHandler(v); err != nil {
                            return err
                        }
                        vc.SetValue(key, v.Value)
                        return nil
                    }
                }
            }
        case "log":
            if m.OnValue == nil {
                m.OnValue = m.wrapHandler()
            }
        default:
            log.Printf("unknown storage.file_type %q (expected log/csv/json/db and combinations like json+csv/json+db/csv+db)", ft)
        }
    }

    // worker limit
    maxW := m.Cfg.System.Processing.MaxWorkers
    if maxW <= 0 {
        maxW = 10
    }
    sem := make(chan struct{}, maxW)

	var wg sync.WaitGroup

	for _, srv := range m.Cfg.Servers {
		if !srv.Enabled {
			continue
		}
		for _, dev := range srv.Devices {
			// apply frequency override if present
			if d, ok := m.Cfg.Frequency[srv.ServerID]; ok && d > 0 {
				dev.PollInterval = d
			}

			collector := &Collector{
				Server:  srv,
				Device:  dev,
				Handler: m.wrapHandler(),
			}

			wg.Add(1)
			go func(c *Collector) {
				defer wg.Done()
				// acquire worker slot
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					return
				}
				if err := c.Run(ctx); err != nil {
					log.Printf("collector stopped (%s/%s): %v", c.Server.ServerID, c.Device.DeviceID, err)
				}
			}(collector)
		}
	}

    // wait until context done, then wait goroutines finish
    <-ctx.Done()
    // give collectors a small grace period to exit their loops
    done := make(chan struct{})
    go func() { wg.Wait(); close(done) }()
    select {
    case <-done:
    case <-time.After(5 * time.Second):
        log.Printf("timeout waiting for collectors to stop")
    }
    if storeClose != nil {
        storeClose()
    }
    return nil
}

func (m *Manager) wrapHandler() ResultHandler {
    if m.OnValue == nil {
        // default: log to stdout
        return func(v PointValue) error {
            log.Printf("%s %s %s[%d] %f %s", v.ServerID, v.DeviceID, v.PointName, v.Address, v.Value, v.Unit)
            return nil
        }
    }
    return m.OnValue
}

// initDatabaseFromConfig populates servers and devices tables from the loaded config
// when the servers table is currently empty. It is safe to call multiple times.
func (m *Manager) initDatabaseFromConfig(db *dbpkg.DB) error {
    // Check if servers table has any rows
    var count int
    if err := db.SQL.QueryRow("SELECT COUNT(*) FROM servers").Scan(&count); err != nil {
        return err
    }
    if count > 0 {
        return nil
    }

    tx, err := db.SQL.Begin()
    if err != nil {
        return err
    }
    defer func() {
        if err != nil {
            _ = tx.Rollback()
        }
    }()

    // Insert servers
    for _, srv := range m.Cfg.Servers {
        var pollStr string
        if d, ok := m.Cfg.Frequency[srv.ServerID]; ok && d > 0 {
            pollStr = d.String()
        }
        _, err = tx.Exec(
            `INSERT OR IGNORE INTO servers
            (server_id, server_name, protocol, host, port, timeout, retry_count, enabled, poll_interval)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
            srv.ServerID,
            srv.ServerName,
            strings.ToLower(strings.TrimSpace(srv.Protocol)),
            srv.Connection.Host,
            srv.Connection.Port,
            srv.Timeout.String(),
            srv.RetryCount,
            srv.Enabled,
            pollStr,
        )
        if err != nil {
            return err
        }
        // Insert devices for this server
        for _, dev := range srv.Devices {
            _, err = tx.Exec(
                `INSERT OR REPLACE INTO devices
                (device_id, server_id, vendor, slave_id, poll_interval)
                VALUES (?, ?, ?, ?, ?)`,
                dev.DeviceID,
                srv.ServerID,
                dev.Vendor,
                int64(dev.SlaveID),
                dev.PollInterval.String(),
            )
            if err != nil {
                return err
            }
        }
    }

    if err = tx.Commit(); err != nil {
        return err
    }
    return nil
}

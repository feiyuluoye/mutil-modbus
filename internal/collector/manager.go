package collector

import (
    "context"
    "log"
    "strings"
    "sync"
    "time"
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
        case "", "csv", "json", "jsonl", "json+csv", "csv+json", "both", "all":
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
                storeHandler := store.Handle
                if m.OnValue == nil {
                    m.OnValue = storeHandler
                } else {
                    userH := m.OnValue
                    m.OnValue = func(v PointValue) error {
                        if err := userH(v); err != nil {
                            log.Printf("custom handler error: %v", err)
                        }
                        return storeHandler(v)
                    }
                }
            }
        case "log":
            if m.OnValue == nil {
                m.OnValue = m.wrapHandler()
            }
        default:
            log.Printf("unknown storage.file_type %q (expected log/csv/json/json+csv)", ft)
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

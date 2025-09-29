package collector

import (
	"context"
	"log"
	"sync"
	"time"
)

// Manager coordinates running multiple device collectors concurrently.

type Manager struct {
	Cfg     RootConfig
	OnValue ResultHandler // optional global handler
}

func (m *Manager) Run(ctx context.Context) error {
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

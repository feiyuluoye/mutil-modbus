package servermgr

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	collector "modbus-simulator/internal/collector"
	"modbus-simulator/internal/modbus"
)

// Manager spins up multiple Modbus servers concurrently from YAML config.
// Currently supports Modbus TCP based on collector.ServerConfig.
// It initializes registers defined by devices/points to zero values.
type Manager struct {
	Cfg     collector.RootConfig
	servers map[string]*modbus.Server
	mu      sync.Mutex
}

func NewManager(cfg collector.RootConfig) *Manager {
	return &Manager{Cfg: cfg, servers: make(map[string]*modbus.Server)}
}

// Run starts all enabled TCP servers and blocks until ctx is canceled.
func (m *Manager) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16) // cap concurrent starts

	for _, srv := range m.Cfg.Servers {
		if !srv.Enabled {
			continue
		}
		proto := strings.ToLower(strings.TrimSpace(srv.Protocol))
		if proto != "modbus-tcp" && proto != "tcp" {
			log.Printf("server %s: protocol %s not supported yet (skipping)", srv.ServerID, srv.Protocol)
			continue
		}

		wg.Add(1)
		go func(s collector.ServerConfig) {
			defer wg.Done()
			select { case sem <- struct{}{}: defer func(){ <-sem }(); case <-ctx.Done(): return }

			addr := fmt.Sprintf("%s:%d", s.Connection.Host, s.Connection.Port)
			retry := s.RetryCount
			if retry < 0 { retry = 0 }

			var server *modbus.Server
			var err error
			for attempt := 0; attempt <= retry; attempt++ {
				server = modbus.NewServer()
				if err = server.Listen(addr); err != nil {
					if attempt == retry {
						log.Printf("server %s listen %s failed: %v", s.ServerID, addr, err)
						return
					}
					time.Sleep(time.Second)
					continue
				}
				break
			}

			m.mu.Lock()
			m.servers[s.ServerID] = server
			m.mu.Unlock()

			log.Printf("server %s listening on %s", s.ServerID, addr)

			// initialize registers for declared points to zero values
			for _, dev := range s.Devices {
				for _, p := range dev.Points {
					switch strings.ToLower(p.RegisterType) {
					case "holding":
						_ = server.SetHoldingRegister(p.Address, 0)
					case "input":
						_ = server.SetInputRegister(p.Address, 0)
					case "coil":
						_ = server.SetCoil(p.Address, false)
					case "discrete":
						_ = server.SetDiscreteInput(p.Address, false)
					}
				}
			}

			// wait for context cancellation, then close
			<-ctx.Done()
			server.Close()
			m.mu.Lock()
			delete(m.servers, s.ServerID)
			m.mu.Unlock()
			log.Printf("server %s stopped", s.ServerID)
		}(srv)
	}

	// wait for ctx canceled then wait workers
	<-ctx.Done()
	wg.Wait()
	return nil
}

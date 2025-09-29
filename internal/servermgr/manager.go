package servermgr

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	collector "modbus-simulator/internal/collector"
	"modbus-simulator/internal/modbus"
	"modbus-simulator/internal/model"
)

// Manager spins up multiple Modbus servers concurrently from YAML config.
// Currently supports Modbus TCP based on collector.ServerConfig.
// It initializes registers defined by devices/points to zero values.
type Manager struct {
	Cfg     collector.RootConfig
	servers map[string]*modbus.Server
	mu      sync.Mutex
}

// loadCSV reads a CSV file where the header row defines column names.
// Returns a slice of rows as map[column]uint16, suitable for uint16/boolean registers.
func loadCSV(path string) ([]map[string]uint16, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return nil, errors.New("csv must contain header and at least one data row")
	}
	header := records[0]
	row := make([]map[string]uint16, 0, len(records)-1)
	for _, rec := range records[1:] {
		if len(rec) != len(header) {
			return nil, errors.New("csv record length mismatch")
		}
		m := make(map[string]uint16, len(header))
		for i, key := range header {
			v, err := strconv.ParseUint(rec[i], 10, 16)
			if err != nil {
				return nil, fmt.Errorf("invalid value for column %s: %w", key, err)
			}
			m[strings.TrimSpace(key)] = uint16(v)
		}
		row = append(row, m)
	}
	return row, nil
}

// applyRowToServer writes one CSV row into the server's registers based on point names.
func applyRowToServer(server *modbus.Server, s collector.ServerConfig, rows []map[string]uint16, index int) {
	if len(rows) == 0 {
		return
	}
	row := rows[index]
	for _, dev := range s.Devices {
		for _, p := range dev.Points {
			key := strings.TrimSpace(p.Name)
			val, ok := row[key]
			if !ok {
				// no matching column; skip
				continue
			}
			switch strings.ToLower(p.RegisterType) {
			case "holding":
				_ = server.SetHoldingRegister(p.Address, val)
			case "input":
				_ = server.SetInputRegister(p.Address, val)
			case "coil":
				_ = server.SetCoil(p.Address, val > 0)
			case "discrete":
				_ = server.SetDiscreteInput(p.Address, val > 0)
			}
		}
	}
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
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			addr := fmt.Sprintf("%s:%d", s.Connection.Host, s.Connection.Port)
			retry := s.RetryCount
			if retry < 0 {
				retry = 0
			}

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

			// Load CSV data and periodically write to registers following cmd/server simulator
			rows, err := loadCSV("data/example_data.csv")
			if err != nil {
				log.Printf("server %s: load csv failed: %v (skipping periodic updates)", s.ServerID, err)
			} else if len(rows) == 0 {
				log.Printf("server %s: csv has no data rows (skipping periodic updates)", s.ServerID)
			} else {
				// interval from frequency map; fallback 3s
				interval := m.Cfg.Frequency[s.ServerID]
				if interval <= 0 {
					interval = 3 * time.Second
				}
				ticker := time.NewTicker(interval)
				defer ticker.Stop()

				// apply first row immediately
				applyRowToServer(server, s, rows, 0)

				go func() {
					idx := 0
					for {
						select {
						case <-ctx.Done():
							return
						case <-ticker.C:
							idx = (idx + 1) % len(rows)
							applyRowToServer(server, s, rows, idx)
						}
					}
				}()
			}

			// wait for context cancellation, then close
			<-ctx.Done()
			server.Close()
			m.mu.Lock()
			delete(m.servers, s.ServerID)
			m.mu.Unlock()
		}(srv)
	}

	// wait for ctx canceled then wait workers
	<-ctx.Done()
	wg.Wait()
	return nil
}

// Snapshot reads current values from running servers and returns server/device/point snapshots.
func (m *Manager) Snapshot() ([]model.ServerSnapshot, error) {
	m.mu.Lock()
	servers := make(map[string]*modbus.Server, len(m.servers))
	for k, v := range m.servers {
		servers[k] = v
	}
	m.mu.Unlock()

	res := make([]model.ServerSnapshot, 0, len(servers))
	now := time.Now()

	for _, sc := range m.Cfg.Servers {
		s := servers[sc.ServerID]
		if s == nil || !sc.Enabled {
			continue
		}

		snap := model.ServerSnapshot{
			ServerID:   sc.ServerID,
			ServerName: sc.ServerName,
			Address:    fmt.Sprintf("%s:%d", sc.Connection.Host, sc.Connection.Port),
			Timestamp:  now,
		}

		for _, dev := range sc.Devices {
			ds := model.DeviceSnapshot{
				DeviceID: dev.DeviceID,
				Vendor:   dev.Vendor,
				SlaveID:  dev.SlaveID,
			}
			for _, p := range dev.Points {
				ps := model.PointSnapshot{
					Name:         p.Name,
					RegisterType: strings.ToLower(p.RegisterType),
					Address:      p.Address,
					Unit:         p.Unit,
					Timestamp:    now,
				}
				switch ps.RegisterType {
				case "holding":
					if v, err := modbusGetU16(s, "holding", p.Address); err == nil {
						ps.ValueUint16 = &v
					}
				case "input":
					if v, err := modbusGetU16(s, "input", p.Address); err == nil {
						ps.ValueUint16 = &v
					}
				case "coil":
					if b, err := modbusGetBool(s, "coil", p.Address); err == nil {
						ps.ValueBool = &b
					}
				case "discrete":
					if b, err := modbusGetBool(s, "discrete", p.Address); err == nil {
						ps.ValueBool = &b
					}
				}
				ds.Points = append(ds.Points, ps)
			}
			snap.Devices = append(snap.Devices, ds)
		}
		res = append(res, snap)
	}
	return res, nil
}

func modbusGetU16(s *modbus.Server, kind string, addr uint16) (uint16, error) {
	switch strings.ToLower(kind) {
	case "holding":
		return modbus.GetHoldingRegister(s, addr)
	case "input":
		return modbus.GetInputRegister(s, addr)
	default:
		return 0, fmt.Errorf("unsupported kind %s", kind)
	}
}

func modbusGetBool(s *modbus.Server, kind string, addr uint16) (bool, error) {
	switch strings.ToLower(kind) {
	case "coil":
		return modbus.GetCoil(s, addr)
	case "discrete":
		return modbus.GetDiscreteInput(s, addr)
	default:
		return false, fmt.Errorf("unsupported kind %s", kind)
	}
}

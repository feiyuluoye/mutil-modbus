package servermgr

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"log"
	"math"
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

// registerValue holds metadata for a single register point
type registerValue struct {
	regType  string
	address  uint16
	column   string
	scale    float64
	offset   float64
	dataType string
}

// loadCSV reads a CSV file where the header row defines column names.
// Returns a slice of rows as map[column]float64, suitable for numeric transformations.
func loadCSV(path string) ([]map[string]float64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return nil, errors.New("csv must contain header and at least one data row")
	}

	header := records[0]
	rows := make([]map[string]float64, 0, len(records)-1)
	for _, record := range records[1:] {
		if len(record) != len(header) {
			return nil, errors.New("csv record length mismatch")
		}
		row := make(map[string]float64, len(header))
		for i, key := range header {
			valStr := strings.TrimSpace(record[i])
			if valStr == "" {
				return nil, fmt.Errorf("empty value for column %s", key)
			}
			val, err := strconv.ParseFloat(valStr, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid value for column %s: %w", key, err)
			}
			row[key] = val
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// applyRowToServer writes one CSV row into the server's registers based on point names.
// Applies scale and offset transformations and supports multiple data types.
func applyRowToServer(server *modbus.Server, s collector.ServerConfig, rows []map[string]float64, index int) {
	if len(rows) == 0 {
		return
	}
	row := rows[index]
	for _, dev := range s.Devices {
		for _, p := range dev.Points {
			key := strings.TrimSpace(p.Name)
			raw, ok := row[key]
			if !ok {
				// no matching column; skip
				continue
			}

			// Apply scale and offset
			scale := p.Scale
			if scale == 0 {
				scale = 1
			}
			scaled := raw*scale + p.Offset

			// Get data type, default to uint16 for numeric registers
			dataType := strings.ToLower(p.DataType)
			regType := strings.ToLower(p.RegisterType)

			switch regType {
			case "holding", "input":
				if dataType == "" {
					dataType = "uint16"
				}
				if err := writeNumericRegister(server, regType, p.Address, dataType, scaled); err != nil {
					log.Printf("set %s register: %v", regType, err)
				}
			case "coil":
				_ = server.SetCoil(p.Address, scaled > 0)
			case "discrete":
				_ = server.SetDiscreteInput(p.Address, scaled > 0)
			}
		}
	}
}

// writeNumericRegister writes a numeric value to a register with the specified data type
func writeNumericRegister(server *modbus.Server, regType string, address uint16, dataType string, scaled float64) error {
	switch dataType {
	case "uint16":
		word, err := floatToUint16(scaled)
		if err != nil {
			return err
		}
		return setRegisterWord(server, regType, address, word)
	case "int16":
		word, err := floatToInt16(scaled)
		if err != nil {
			return err
		}
		return setRegisterWord(server, regType, address, word)
	case "float32":
		return setRegisterFloat32(server, regType, address, scaled)
	default:
		return fmt.Errorf("unsupported data type %s", dataType)
	}
}

// setRegisterWord sets a single 16-bit word to a register
func setRegisterWord(server *modbus.Server, regType string, address uint16, word uint16) error {
	switch regType {
	case "holding":
		return server.SetHoldingRegister(address, word)
	case "input":
		return server.SetInputRegister(address, word)
	default:
		return fmt.Errorf("register type %s does not support word writes", regType)
	}
}

// setRegisterFloat32 writes a float32 value across two consecutive registers
func setRegisterFloat32(server *modbus.Server, regType string, address uint16, scaled float64) error {
	if math.IsNaN(scaled) || math.IsInf(scaled, 0) {
		return fmt.Errorf("invalid float32 value")
	}
	if address == math.MaxUint16 {
		return fmt.Errorf("address %d out of range for float32", address)
	}
	f32 := float32(scaled)
	if math.IsInf(float64(f32), 0) {
		return fmt.Errorf("value %f overflows float32", scaled)
	}
	bits := math.Float32bits(f32)
	hi := uint16(bits >> 16)
	lo := uint16(bits & 0xFFFF)
	if err := setRegisterWord(server, regType, address, hi); err != nil {
		return err
	}
	return setRegisterWord(server, regType, address+1, lo)
}

// floatToUint16 converts a float64 to uint16 with range checking
func floatToUint16(value float64) (uint16, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("invalid uint16 value")
	}
	rounded := math.Round(value)
	if rounded < 0 || rounded > 65535 {
		return 0, fmt.Errorf("value %f out of range for uint16", value)
	}
	return uint16(rounded), nil
}

// floatToInt16 converts a float64 to int16 with range checking
func floatToInt16(value float64) (uint16, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("invalid int16 value")
	}
	rounded := math.Round(value)
	if rounded < -32768 || rounded > 32767 {
		return 0, fmt.Errorf("value %f out of range for int16", value)
	}
	return uint16(int16(rounded)), nil
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
			// Use CSV file from config or default to data/topway_dashboard.csv
			csvPath := s.CSVFile
			if csvPath == "" {
				csvPath = "data/topway_dashboard.csv"
			}
			rows, err := loadCSV(csvPath)
			if err != nil {
				log.Printf("server %s: load csv %s failed: %v (skipping periodic updates)", s.ServerID, csvPath, err)
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

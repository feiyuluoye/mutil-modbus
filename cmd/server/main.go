package main

import (
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"modbus-simulator/internal/config"
	"modbus-simulator/internal/modbus"
)

type registerValue struct {
	regType  string
	address  uint16
	column   string
	scale    float64
	offset   float64
	dataType string
}

type simulator struct {
	cfg          config.Config
	server       *modbus.Server
	values       []registerValue
	dataRows     []map[string]float64
	updatePeriod time.Duration
	mu           sync.Mutex
	rowIndex     int
}

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "config.toml", "Path to configuration file")
	flag.Parse()

	if err := run(configPath); err != nil {
		log.Fatal(err)
	}
}

func run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	sim, err := newSimulator(cfg)
	if err != nil {
		return fmt.Errorf("create simulator: %w", err)
	}
	defer sim.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sim.Start(ctx)
	}()

	select {
	case <-ctx.Done():
		log.Println("shutting down simulator")
		return nil
	case err := <-errCh:
		return err
	}
}

func newSimulator(cfg config.Config) (*simulator, error) {
	duration, err := time.ParseDuration(cfg.UpdateInterval)
	if err != nil {
		return nil, fmt.Errorf("invalid update interval: %w", err)
	}

	server := modbus.NewServer()
	if err := server.Listen(cfg.Server.ListenAddress); err != nil {
		return nil, fmt.Errorf("start modbus server: %w", err)
	}

	values := make([]registerValue, len(cfg.Registers))
	for i, reg := range cfg.Registers {
		switch reg.Type {
		case "holding", "input", "coil", "discrete":
		default:
			server.Close()
			return nil, fmt.Errorf("unsupported register type %s", reg.Type)
		}
		dataType := strings.ToLower(reg.DataType)
		switch reg.Type {
		case "holding", "input":
			if dataType == "" {
				dataType = "uint16"
			}
			switch dataType {
			case "uint16", "int16", "float32":
			default:
				server.Close()
				return nil, fmt.Errorf("unsupported data_type %s for %s register", dataType, reg.Type)
			}
		case "coil", "discrete":
			if dataType != "" {
				server.Close()
				return nil, fmt.Errorf("data_type not supported for %s registers", reg.Type)
			}
		}

		scale := reg.Scale
		if scale == 0 {
			scale = 1
		}
		values[i] = registerValue{
			regType:  reg.Type,
			address:  reg.Address,
			column:   reg.CSVColumn,
			scale:    scale,
			offset:   reg.Offset,
			dataType: dataType,
		}
	}

	rows, err := loadCSV(cfg.CSVFile)
	if err != nil {
		server.Close()
		return nil, fmt.Errorf("load csv: %w", err)
	}

	sim := &simulator{
		cfg:          cfg,
		server:       server,
		values:       values,
		dataRows:     rows,
		updatePeriod: duration,
	}

	return sim, nil
}

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
func (s *simulator) Start(ctx context.Context) error {
	ticker := time.NewTicker(s.updatePeriod)
	defer ticker.Stop()

	log.Printf("Modbus simulator listening on %s", s.cfg.Server.ListenAddress)

	s.applyRow(0)

	for {
		select {
		case <-ticker.C:
			s.nextRow()
		case <-ctx.Done():
			return nil
		}
	}
}

func (s *simulator) nextRow() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.dataRows) == 0 {
		return
	}

	s.rowIndex = (s.rowIndex + 1) % len(s.dataRows)
	s.applyRowLocked(s.rowIndex)
}

func (s *simulator) applyRow(index int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyRowLocked(index)
}

func (s *simulator) applyRowLocked(index int) {
	if len(s.dataRows) == 0 {
		return
	}
	row := s.dataRows[index]
	for _, value := range s.values {
		raw, ok := row[value.column]
		if !ok {
			log.Printf("column %s not found in csv data", value.column)
			continue
		}

		scaled := raw*value.scale + value.offset
		switch value.regType {
		case "holding":
			if err := s.writeNumericRegister(value, scaled); err != nil {
				log.Printf("set holding register: %v", err)
			}
		case "input":
			if err := s.writeNumericRegister(value, scaled); err != nil {
				log.Printf("set input register: %v", err)
			}
		case "coil":
			if err := s.server.SetCoil(value.address, scaled > 0); err != nil {
				log.Printf("set coil: %v", err)
			}
		case "discrete":
			if err := s.server.SetDiscreteInput(value.address, scaled > 0); err != nil {
				log.Printf("set discrete input: %v", err)
			}
		default:
			log.Printf("unsupported register type %s", value.regType)
		}
	}
}

func (s *simulator) writeNumericRegister(v registerValue, scaled float64) error {
	switch v.dataType {
	case "uint16":
		word, err := floatToUint16(scaled)
		if err != nil {
			return err
		}
		return s.setRegisterWord(v.regType, v.address, word)
	case "int16":
		word, err := floatToInt16(scaled)
		if err != nil {
			return err
		}
		return s.setRegisterWord(v.regType, v.address, word)
	case "float32":
		return s.setRegisterFloat32(v, scaled)
	default:
		return fmt.Errorf("unsupported data type %s", v.dataType)
	}
}

func (s *simulator) setRegisterWord(regType string, address uint16, word uint16) error {
	switch regType {
	case "holding":
		return s.server.SetHoldingRegister(address, word)
	case "input":
		return s.server.SetInputRegister(address, word)
	default:
		return fmt.Errorf("register type %s does not support word writes", regType)
	}
}

func (s *simulator) setRegisterFloat32(v registerValue, scaled float64) error {
	if math.IsNaN(scaled) || math.IsInf(scaled, 0) {
		return fmt.Errorf("invalid float32 value for column %s", v.column)
	}
	if v.address == math.MaxUint16 {
		return fmt.Errorf("address %d out of range for float32", v.address)
	}
	f32 := float32(scaled)
	if math.IsInf(float64(f32), 0) {
		return fmt.Errorf("value %f overflows float32", scaled)
	}
	bits := math.Float32bits(f32)
	hi := uint16(bits >> 16)
	lo := uint16(bits & 0xFFFF)
	if err := s.setRegisterWord(v.regType, v.address, hi); err != nil {
		return err
	}
	return s.setRegisterWord(v.regType, v.address+1, lo)
}

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

func (s *simulator) Close() {
	s.server.Close()
}

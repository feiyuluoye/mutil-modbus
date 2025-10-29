package main

import (
	"context"
	"encoding/csv"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
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

	"github.com/goburrow/serial"
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
	tcpServer    *modbus.Server
	rw           registerWriter
	values       []registerValue
	dataRows     []map[string]float64
	updatePeriod time.Duration
	mu           sync.Mutex
	rowIndex     int
	rtuCancel    context.CancelFunc
}

func main() {
	var configPath string
	var rtuMode bool
	flag.StringVar(&configPath, "config", "config.toml", "Path to configuration file")
	flag.BoolVar(&rtuMode, "rtu", false, "Enable Modbus RTU (serial) mode")
	flag.Parse()

	if err := run(configPath, rtuMode); err != nil {
		log.Fatal(err)
	}
}

func run(configPath string, rtuMode bool) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	sim, err := newSimulator(cfg)
	if err != nil {
		return fmt.Errorf("create simulator: %w", err)
	}
	// Auto-enable RTU if requested via flag or config
	if rtuMode || strings.ToLower(cfg.Server.Mode) == "rtu" || cfg.Server.SerialPort != "" {
		if err := enableRTUModeFromConfig(sim, cfg); err != nil {
			return fmt.Errorf("enable RTU: %w", err)
		}
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
		tcpServer:    server,
		rw:           server,
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

	if s.tcpServer != nil {
		log.Printf("Modbus simulator listening on %s", s.cfg.Server.ListenAddress)
	} else {
		log.Printf("Modbus RTU simulator started")
	}

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
			if err := s.rw.SetCoil(value.address, scaled > 0); err != nil {
				log.Printf("set coil: %v", err)
			}
		case "discrete":
			if err := s.rw.SetDiscreteInput(value.address, scaled > 0); err != nil {
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
		return s.rw.SetHoldingRegister(address, word)
	case "input":
		return s.rw.SetInputRegister(address, word)
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
	if s.tcpServer != nil {
		s.tcpServer.Close()
	}
	if s.rtuCancel != nil {
		s.rtuCancel()
	}
}

type registerWriter interface {
	SetHoldingRegister(address uint16, value uint16) error
	SetInputRegister(address uint16, value uint16) error
	SetCoil(address uint16, value bool) error
	SetDiscreteInput(address uint16, value bool) error
}

// --- RTU mode support (serial) ---
// Switch simulator to RTU mode by using a local RTU store and starting a serial stream handler.
func enableRTUModeFromConfig(s *simulator, cfg config.Config) error {
	st := newRTUStore()
	s.tcpServer = nil
	s.rw = st

	// Load serial params from cfg.Server
	ser := serialParams{ Address: cfg.Server.SerialPort, Baud: cfg.Server.BaudRate, DataBits: cfg.Server.DataBits, StopBits: cfg.Server.StopBits, Parity: cfg.Server.Parity }
	if ser.Baud == 0 { ser.Baud = 9600 }
	if ser.DataBits == 0 { ser.DataBits = 8 }
	if ser.StopBits == 0 { ser.StopBits = 1 }
	if ser.Parity == "" { ser.Parity = "N" }
	if ser.Address == "" {
		return fmt.Errorf("serial_port must be set in [server] for RTU mode")
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.rtuCancel = cancel
	go func() {
		if err := serveSerialRTU(ctx, ser, st); err != nil {
			log.Printf("rtu handler error: %v", err)
		}
	}()
	return nil
}

// Local RTU in-memory store implements registerWriter
type rtuStore struct {
	mu        sync.RWMutex
	coils     []bool
	discretes []bool
	holding   []uint16
	input     []uint16
}

func newRTUStore() *rtuStore {
	return &rtuStore{
		coils:     make([]bool, 65536),
		discretes: make([]bool, 65536),
		holding:   make([]uint16, 65536),
		input:     make([]uint16, 65536),
	}
}

func (s *rtuStore) SetHoldingRegister(a uint16, v uint16) error { s.mu.Lock(); s.holding[a] = v; s.mu.Unlock(); return nil }
func (s *rtuStore) SetInputRegister(a uint16, v uint16) error   { s.mu.Lock(); s.input[a] = v; s.mu.Unlock(); return nil }
func (s *rtuStore) SetCoil(a uint16, v bool) error               { s.mu.Lock(); s.coils[a] = v; s.mu.Unlock(); return nil }
func (s *rtuStore) SetDiscreteInput(a uint16, v bool) error      { s.mu.Lock(); s.discretes[a] = v; s.mu.Unlock(); return nil }

// PDU helpers
func rtuReadBits(src []bool, start, qty uint16) ([]byte, error) {
	if qty == 0 || qty > 2000 { return nil, fmt.Errorf("invalid qty") }
	end := int(start) + int(qty)
	if end > len(src) { return nil, fmt.Errorf("out of range") }
	bc := (int(qty) + 7) / 8
	res := make([]byte, bc)
	for i := 0; i < int(qty); i++ {
		if src[int(start)+i] { res[i/8] |= 1 << (uint(i) % 8) }
	}
	return res, nil
}
func rtuReadRegs(src []uint16, start, qty uint16) ([]byte, error) {
	if qty == 0 || qty > 125 { return nil, fmt.Errorf("invalid qty") }
	end := int(start) + int(qty)
	if end > len(src) { return nil, fmt.Errorf("out of range") }
	res := make([]byte, qty*2)
	for i := 0; i < int(qty); i++ { binary.BigEndian.PutUint16(res[i*2:(i+1)*2], src[int(start)+i]) }
	return res, nil
}
func rtuWriteSingleCoil(dst []bool, addr uint16, value uint16) error {
	if int(addr) >= len(dst) { return fmt.Errorf("out of range") }
	if value != 0xFF00 && value != 0x0000 { return fmt.Errorf("invalid value") }
	dst[addr] = value == 0xFF00
	return nil
}
func rtuWriteSingleReg(dst []uint16, addr uint16, value uint16) error {
	if int(addr) >= len(dst) { return fmt.Errorf("out of range") }
	dst[addr] = value
	return nil
}
func rtuWriteMultipleCoils(dst []bool, start, qty uint16, payload []byte) error {
	if qty == 0 || qty > 1968 { return fmt.Errorf("invalid qty") }
	end := int(start) + int(qty)
	if end > len(dst) { return fmt.Errorf("out of range") }
	for i := 0; i < int(qty); i++ {
		bit := (payload[i/8] >> (uint(i) % 8)) & 0x01
		dst[int(start)+i] = bit == 0x01
	}
	return nil
}
func rtuWriteMultipleRegs(dst []uint16, start, qty uint16, payload []byte) error {
	if qty == 0 || qty > 123 { return fmt.Errorf("invalid qty") }
	if len(payload) != int(qty)*2 { return fmt.Errorf("invalid byte count") }
	end := int(start) + int(qty)
	if end > len(dst) { return fmt.Errorf("out of range") }
	for i := 0; i < int(qty); i++ {
		v := binary.BigEndian.Uint16(payload[i*2 : (i+1)*2])
		dst[int(start)+i] = v
	}
	return nil
}

func rtuException(fn byte) []byte { return []byte{fn | 0x80, 0x02} }

func handleRTUPDU(st *rtuStore, pdu []byte) ([]byte, error) {
	if len(pdu) < 1 { return nil, fmt.Errorf("empty pdu") }
	fn := pdu[0]
	st.mu.RLock(); defer st.mu.RUnlock()
	switch fn {
	case 0x01:
		if len(pdu) < 5 { return nil, fmt.Errorf("invalid pdu") }
		start := binary.BigEndian.Uint16(pdu[1:3])
		qty := binary.BigEndian.Uint16(pdu[3:5])
		data, err := rtuReadBits(st.coils, start, qty)
		if err != nil { return rtuException(fn), nil }
		return append([]byte{fn, byte(len(data))}, data...), nil
	case 0x02:
		if len(pdu) < 5 { return nil, fmt.Errorf("invalid pdu") }
		start := binary.BigEndian.Uint16(pdu[1:3])
		qty := binary.BigEndian.Uint16(pdu[3:5])
		data, err := rtuReadBits(st.discretes, start, qty)
		if err != nil { return rtuException(fn), nil }
		return append([]byte{fn, byte(len(data))}, data...), nil
	case 0x03:
		if len(pdu) < 5 { return nil, fmt.Errorf("invalid pdu") }
		start := binary.BigEndian.Uint16(pdu[1:3])
		qty := binary.BigEndian.Uint16(pdu[3:5])
		data, err := rtuReadRegs(st.holding, start, qty)
		if err != nil { return rtuException(fn), nil }
		return append([]byte{fn, byte(len(data))}, data...), nil
	case 0x04:
		if len(pdu) < 5 { return nil, fmt.Errorf("invalid pdu") }
		start := binary.BigEndian.Uint16(pdu[1:3])
		qty := binary.BigEndian.Uint16(pdu[3:5])
		data, err := rtuReadRegs(st.input, start, qty)
		if err != nil { return rtuException(fn), nil }
		return append([]byte{fn, byte(len(data))}, data...), nil
	case 0x05:
		if len(pdu) < 5 { return nil, fmt.Errorf("invalid pdu") }
		addr := binary.BigEndian.Uint16(pdu[1:3])
		val := binary.BigEndian.Uint16(pdu[3:5])
		st.mu.RUnlock(); st.mu.Lock(); defer func(){ st.mu.Unlock(); st.mu.RLock() }()
		if err := rtuWriteSingleCoil(st.coils, addr, val); err != nil { return rtuException(fn), nil }
		return append([]byte{fn}, pdu[1:5]...), nil
	case 0x06:
		if len(pdu) < 5 { return nil, fmt.Errorf("invalid pdu") }
		addr := binary.BigEndian.Uint16(pdu[1:3])
		val := binary.BigEndian.Uint16(pdu[3:5])
		st.mu.RUnlock(); st.mu.Lock(); defer func(){ st.mu.Unlock(); st.mu.RLock() }()
		if err := rtuWriteSingleReg(st.holding, addr, val); err != nil { return rtuException(fn), nil }
		return append([]byte{fn}, pdu[1:5]...), nil
	case 0x0F:
		if len(pdu) < 6 { return nil, fmt.Errorf("invalid pdu") }
		start := binary.BigEndian.Uint16(pdu[1:3])
		qty := binary.BigEndian.Uint16(pdu[3:5])
		bc := int(pdu[5])
		if len(pdu) != 6+bc { return nil, fmt.Errorf("invalid pdu len") }
		payload := pdu[6:]
		st.mu.RUnlock(); st.mu.Lock(); defer func(){ st.mu.Unlock(); st.mu.RLock() }()
		if err := rtuWriteMultipleCoils(st.coils, start, qty, payload); err != nil { return rtuException(fn), nil }
		resp := make([]byte, 5)
		resp[0] = fn
		binary.BigEndian.PutUint16(resp[1:3], start)
		binary.BigEndian.PutUint16(resp[3:5], qty)
		return resp, nil
	case 0x10:
		if len(pdu) < 6 { return nil, fmt.Errorf("invalid pdu") }
		start := binary.BigEndian.Uint16(pdu[1:3])
		qty := binary.BigEndian.Uint16(pdu[3:5])
		bc := int(pdu[5])
		if len(pdu) != 6+bc { return nil, fmt.Errorf("invalid pdu len") }
		payload := pdu[6:]
		st.mu.RUnlock(); st.mu.Lock(); defer func(){ st.mu.Unlock(); st.mu.RLock() }()
		if err := rtuWriteMultipleRegs(st.holding, start, qty, payload); err != nil { return rtuException(fn), nil }
		resp := make([]byte, 5)
		resp[0] = fn
		binary.BigEndian.PutUint16(resp[1:3], start)
		binary.BigEndian.PutUint16(resp[3:5], qty)
		return resp, nil
	default:
		return []byte{fn | 0x80, 0x01}, nil
	}
}

func crc16Modbus(data []byte) uint16 {
	var crc uint16 = 0xFFFF
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if (crc & 0x0001) != 0 { crc = (crc >> 1) ^ 0xA001 } else { crc = crc >> 1 }
		}
	}
	return crc
}

type serialParams struct { Address string; Baud, DataBits, StopBits int; Parity string }

func serveSerialRTU(ctx context.Context, sp serialParams, st *rtuStore) error {
	sc := &serial.Config{ Address: sp.Address, BaudRate: sp.Baud, DataBits: sp.DataBits, StopBits: sp.StopBits, Parity: sp.Parity, Timeout: 10 * time.Second }
	if sc.BaudRate == 0 { sc.BaudRate = 9600 }
	if sc.DataBits == 0 { sc.DataBits = 8 }
	if sc.StopBits == 0 { sc.StopBits = 1 }
	if sc.Parity == "" { sc.Parity = "N" }
	rw, err := serial.Open(sc)
	if err != nil { return err }
	defer rw.Close()

	done := make(chan struct{})
	go func(){ defer close(done); rtuStream(rw, st) }()
	<-ctx.Done()
	rw.Close()
	<-done
	return nil
}

// Process RTU frames on a stream (serial ReadWriter)
func rtuStream(rw io.ReadWriter, st *rtuStore) {
	for {
		head := make([]byte, 2)
		if _, err := io.ReadFull(rw, head); err != nil { return }
		addr := head[0]; fn := head[1]
		switch fn {
		case 0x01, 0x02, 0x03, 0x04, 0x05, 0x06:
			rest := make([]byte, 6) // start(2)+qty/val(2)+crc(2)
			if _, err := io.ReadFull(rw, rest); err != nil { return }
			reqNoCRC := append([]byte{addr, fn}, rest[:4]...)
			if crc16Modbus(reqNoCRC) != binary.LittleEndian.Uint16(rest[4:]) { continue }
			pdu := append([]byte{fn}, rest[:4]...)
			respPDU, _ := handleRTUPDU(st, pdu)
			out := append([]byte{addr}, respPDU...)
			tail := make([]byte, 2)
			binary.LittleEndian.PutUint16(tail, crc16Modbus(out))
			out = append(out, tail...)
			_, _ = rw.Write(out)
		case 0x0F, 0x10:
			hdr := make([]byte, 5)
			if _, err := io.ReadFull(rw, hdr); err != nil { return }
			bc := int(hdr[4])
			payload := make([]byte, bc)
			if _, err := io.ReadFull(rw, payload); err != nil { return }
			crcB := make([]byte, 2)
			if _, err := io.ReadFull(rw, crcB); err != nil { return }
			req := append(append(append([]byte{addr, fn}, hdr[:4]...), hdr[4]), payload...)
			if crc16Modbus(req) != binary.LittleEndian.Uint16(crcB) { continue }
			pdu := append([]byte{fn}, append(hdr[:5], payload...)...)
			respPDU, _ := handleRTUPDU(st, pdu)
			out := append([]byte{addr}, respPDU...)
			tail := make([]byte, 2)
			binary.LittleEndian.PutUint16(tail, crc16Modbus(out))
			out = append(out, tail...)
			_, _ = rw.Write(out)
		default:
			return
		}
	}
}

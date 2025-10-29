package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"modbus-simulator/internal/utils"
)

// Config schema: serial-style parameters for documentation, but transport is RTU-over-TCP.
type RootConfig struct {
	Endpoints []Endpoint `yaml:"endpoints"`
}

type Endpoint struct {
	Name           string        `yaml:"name"`
	Mode           string        `yaml:"mode"`           // "rtu_over_tcp" | "serial" (optional; auto-detect if empty)
	ListenAddress  string        `yaml:"listen_address"` // RTU-over-TCP, e.g. 0.0.0.0:5020
	SerialPort     string        `yaml:"serial_port"`    // Real/virtual serial port for Scheme #1 (e.g., /tmp/vport1, COM10)
	SlaveID        uint8         `yaml:"slave_id"`      // 1..247
	BaudRate       int           `yaml:"baud_rate"`     // optional
	DataBits       int           `yaml:"data_bits"`     // optional
	StopBits       int           `yaml:"stop_bits"`     // optional
	Parity         string        `yaml:"parity"`        // N,E,O - optional
	UpdateInterval time.Duration `yaml:"update_interval"`

	// Optional: auto-create a virtual serial pair via socat (Unix-like systems)
	SpawnSocat     bool   `yaml:"spawn_socat"`
	SocatLink      string `yaml:"socat_link"` // path used by this endpoint, e.g., /tmp/vport1
	SocatPeer      string `yaml:"socat_peer"` // peer path for client tool, e.g., /tmp/vport2
}

func loadConfig(path string) (RootConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return RootConfig{}, err
	}
	var cfg RootConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return RootConfig{}, err
	}
	for i := range cfg.Endpoints {
		if cfg.Endpoints[i].SlaveID == 0 {
			cfg.Endpoints[i].SlaveID = 1
		}
		if cfg.Endpoints[i].UpdateInterval <= 0 {
			cfg.Endpoints[i].UpdateInterval = 5 * time.Second
		}
	}
	return cfg, nil
}

// --- Simple register store for one endpoint ---
type store struct {
	mu              sync.RWMutex
	coils           []bool
	discreteInputs  []bool
	holding         []uint16
	input           []uint16
}

func newStore() *store {
	return &store{
		coils:          make([]bool, 65536),
		discreteInputs: make([]bool, 65536),
		holding:        make([]uint16, 65536),
		input:          make([]uint16, 65536),
	}
}

func (s *store) setHolding(addr uint16, v uint16) { s.mu.Lock(); s.holding[addr] = v; s.mu.Unlock() }
func (s *store) setInput(addr uint16, v uint16)   { s.mu.Lock(); s.input[addr] = v; s.mu.Unlock() }
func (s *store) setCoil(addr uint16, v bool)      { s.mu.Lock(); s.coils[addr] = v; s.mu.Unlock() }
func (s *store) setDiscrete(addr uint16, v bool)  { s.mu.Lock(); s.discreteInputs[addr] = v; s.mu.Unlock() }

// --- RTU PDU handlers (function codes) ---
func readBits(src []bool, start, qty uint16) ([]byte, error) {
	if qty == 0 || qty > 2000 { return nil, fmt.Errorf("invalid qty") }
	end := int(start) + int(qty)
	if end > len(src) { return nil, fmt.Errorf("out of range") }
	byteCount := (int(qty) + 7) / 8
	res := make([]byte, byteCount)
	for i := 0; i < int(qty); i++ {
		if src[int(start)+i] { res[i/8] |= 1 << (uint(i) % 8) }
	}
	return res, nil
}

func readRegs(src []uint16, start, qty uint16) ([]byte, error) {
	if qty == 0 || qty > 125 { return nil, fmt.Errorf("invalid qty") }
	end := int(start) + int(qty)
	if end > len(src) { return nil, fmt.Errorf("out of range") }
	res := make([]byte, qty*2)
	for i := 0; i < int(qty); i++ {
		binary.BigEndian.PutUint16(res[i*2:(i+1)*2], src[int(start)+i])
	}
	return res, nil
}

func writeSingleCoil(dst []bool, addr uint16, value uint16) error {
	if int(addr) >= len(dst) { return fmt.Errorf("out of range") }
	if value != 0xFF00 && value != 0x0000 { return fmt.Errorf("invalid value") }
	dst[addr] = value == 0xFF00
	return nil
}

func writeSingleReg(dst []uint16, addr uint16, value uint16) error {
	if int(addr) >= len(dst) { return fmt.Errorf("out of range") }
	dst[addr] = value
	return nil
}

func writeMultipleCoils(dst []bool, start, qty uint16, payload []byte) error {
	if qty == 0 || qty > 1968 { return fmt.Errorf("invalid qty") }
	end := int(start) + int(qty)
	if end > len(dst) { return fmt.Errorf("out of range") }
	for i := 0; i < int(qty); i++ {
		bit := (payload[i/8] >> (uint(i) % 8)) & 0x01
		dst[int(start)+i] = bit == 0x01
	}
	return nil
}

func writeMultipleRegs(dst []uint16, start, qty uint16, payload []byte) error {
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

// handleRTUPDU returns response PDU for given request PDU (without slave id / crc)
func handleRTUPDU(st *store, pdu []byte) ([]byte, error) {
	if len(pdu) < 1 { return nil, fmt.Errorf("empty pdu") }
	fn := pdu[0]
	st.mu.RLock()
	defer st.mu.RUnlock()
	switch fn {
	case 0x01: // Read Coils
		if len(pdu) < 5 { return nil, fmt.Errorf("invalid pdu") }
		start := binary.BigEndian.Uint16(pdu[1:3])
		qty := binary.BigEndian.Uint16(pdu[3:5])
		data, err := readBits(st.coils, start, qty)
		if err != nil { return exception(fn, err), nil }
		return append([]byte{fn, byte(len(data))}, data...), nil
	case 0x02: // Read Discrete Inputs
		if len(pdu) < 5 { return nil, fmt.Errorf("invalid pdu") }
		start := binary.BigEndian.Uint16(pdu[1:3])
		qty := binary.BigEndian.Uint16(pdu[3:5])
		data, err := readBits(st.discreteInputs, start, qty)
		if err != nil { return exception(fn, err), nil }
		return append([]byte{fn, byte(len(data))}, data...), nil
	case 0x03: // Read Holding Registers
		if len(pdu) < 5 { return nil, fmt.Errorf("invalid pdu") }
		start := binary.BigEndian.Uint16(pdu[1:3])
		qty := binary.BigEndian.Uint16(pdu[3:5])
		data, err := readRegs(st.holding, start, qty)
		if err != nil { return exception(fn, err), nil }
		return append([]byte{fn, byte(len(data))}, data...), nil
	case 0x04: // Read Input Registers
		if len(pdu) < 5 { return nil, fmt.Errorf("invalid pdu") }
		start := binary.BigEndian.Uint16(pdu[1:3])
		qty := binary.BigEndian.Uint16(pdu[3:5])
		data, err := readRegs(st.input, start, qty)
		if err != nil { return exception(fn, err), nil }
		return append([]byte{fn, byte(len(data))}, data...), nil
	case 0x05: // Write Single Coil
		if len(pdu) < 5 { return nil, fmt.Errorf("invalid pdu") }
		addr := binary.BigEndian.Uint16(pdu[1:3])
		val := binary.BigEndian.Uint16(pdu[3:5])
		st.mu.RUnlock(); st.mu.Lock(); defer func(){ st.mu.Unlock(); st.mu.RLock() }()
		if err := writeSingleCoil(st.coils, addr, val); err != nil { return exception(fn, err), nil }
		return append([]byte{fn}, pdu[1:5]...), nil
	case 0x06: // Write Single Register
		if len(pdu) < 5 { return nil, fmt.Errorf("invalid pdu") }
		addr := binary.BigEndian.Uint16(pdu[1:3])
		val := binary.BigEndian.Uint16(pdu[3:5])
		st.mu.RUnlock(); st.mu.Lock(); defer func(){ st.mu.Unlock(); st.mu.RLock() }()
		if err := writeSingleReg(st.holding, addr, val); err != nil { return exception(fn, err), nil }
		return append([]byte{fn}, pdu[1:5]...), nil
	case 0x0F: // Write Multiple Coils
		if len(pdu) < 6 { return nil, fmt.Errorf("invalid pdu") }
		start := binary.BigEndian.Uint16(pdu[1:3])
		qty := binary.BigEndian.Uint16(pdu[3:5])
		bc := int(pdu[5])
		if len(pdu) != 6+bc { return nil, fmt.Errorf("invalid pdu len") }
		payload := pdu[6:]
		st.mu.RUnlock(); st.mu.Lock(); defer func(){ st.mu.Unlock(); st.mu.RLock() }()
		if err := writeMultipleCoils(st.coils, start, qty, payload); err != nil { return exception(fn, err), nil }
		resp := make([]byte, 5)
		resp[0] = fn
		binary.BigEndian.PutUint16(resp[1:3], start)
		binary.BigEndian.PutUint16(resp[3:5], qty)
		return resp, nil
	case 0x10: // Write Multiple Registers
		if len(pdu) < 6 { return nil, fmt.Errorf("invalid pdu") }
		start := binary.BigEndian.Uint16(pdu[1:3])
		qty := binary.BigEndian.Uint16(pdu[3:5])
		bc := int(pdu[5])
		if len(pdu) != 6+bc { return nil, fmt.Errorf("invalid pdu len") }
		payload := pdu[6:]
		st.mu.RUnlock(); st.mu.Lock(); defer func(){ st.mu.Unlock(); st.mu.RLock() }()
		if err := writeMultipleRegs(st.holding, start, qty, payload); err != nil { return exception(fn, err), nil }
		resp := make([]byte, 5)
		resp[0] = fn
		binary.BigEndian.PutUint16(resp[1:3], start)
		binary.BigEndian.PutUint16(resp[3:5], qty)
		return resp, nil
	default:
		return []byte{fn | 0x80, 0x01}, nil // illegal function
	}
}

func exception(fn byte, _ error) []byte { return []byte{fn | 0x80, 0x02} }

// crc16Modbus computes Modbus RTU CRC16 over the given bytes.
func crc16Modbus(data []byte) uint16 {
	var crc uint16 = 0xFFFF
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if (crc & 0x0001) != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc = crc >> 1
			}
		}
	}
	return crc
}

// --- RTU-over-TCP connection handler ---
func handleConn(conn net.Conn, st *store, expectSlave uint8) {
	defer conn.Close()
	handleStream(conn, st, expectSlave)
}

// handleStream processes a single RTU stream (TCP conn or serial port)
func handleStream(rw io.ReadWriter, st *store, expectSlave uint8) {
	buf := make([]byte, 0, 300)
	for {
		// Read header: at least address+function
		head := make([]byte, 2)
		if _, err := io.ReadFull(rw, head); err != nil { return }
		address := head[0]
		fn := head[1]
		// Determine request length based on function
		var restLen int
		switch fn {
		case 0x01, 0x02, 0x03, 0x04, 0x05, 0x06:
			restLen = 4 + 2 // start(2)+qty/value(2) + crc(2)
		case 0x0F, 0x10:
			// read header (start(2)+qty(2)+bytecount(1))
			hdr := make([]byte, 5)
			if _, err := io.ReadFull(rw, hdr); err != nil { return }
			byteCount := int(hdr[4])
			payload := make([]byte, byteCount)
			if _, err := io.ReadFull(rw, payload); err != nil { return }
			crcBytes := make([]byte, 2)
			if _, err := io.ReadFull(rw, crcBytes); err != nil { return }
			// Build full request for CRC check
			req := append(append(append([]byte{address, fn}, hdr[:4]...), hdr[4]), payload...)
			// CRC check
			crcCalc := crc16Modbus(req)
			crcRecv := binary.LittleEndian.Uint16(crcBytes)
			if crcCalc != crcRecv { continue }
			if expectSlave != 0 && address != expectSlave { continue }
			// PDU = fn + data(start,qty,bc,payload)
			pdu := append([]byte{fn}, append(hdr[:5], payload...)...)
			respPDU, _ := handleRTUPDU(st, pdu)
			resp := make([]byte, 0, 2+len(respPDU)+2)
			resp = append(resp, address)
			resp = append(resp, respPDU...)
			crc := crc16Modbus(resp)
			crcTail := make([]byte, 2)
			binary.LittleEndian.PutUint16(crcTail, crc)
			resp = append(resp, crcTail...)
			_, _ = rw.Write(resp)
			continue
		default:
			// Unknown; try to drain some bytes and continue
			return
		}

		rest := make([]byte, restLen)
		if _, err := io.ReadFull(rw, rest); err != nil { return }
		// Build request without CRC for calculation
		reqNoCRC := append([]byte{address, fn}, rest[:len(rest)-2]...)
		crcCalc := crc16Modbus(reqNoCRC)
		crcRecv := binary.LittleEndian.Uint16(rest[len(rest)-2:])
		if crcCalc != crcRecv { continue }
		if expectSlave != 0 && address != expectSlave { continue }
		// PDU = fn + data (exclude CRC)
		pdu := append([]byte{fn}, rest[:len(rest)-2]...)
		respPDU, _ := handleRTUPDU(st, pdu)
		buf = buf[:0]
		buf = append(buf, address)
		buf = append(buf, respPDU...)
		crc := crc16Modbus(buf)
		crcTail := make([]byte, 2)
		binary.LittleEndian.PutUint16(crcTail, crc)
		buf = append(buf, crcTail...)
		_, _ = rw.Write(buf)
	}
}

// --- Dynamic updater ---
func startDynamic(st *store, interval time.Duration, stop <-chan struct{}) {
	if interval <= 0 { interval = 5 * time.Second }
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				st.mu.Lock()
				st.holding[100]++ // simple counter
				st.mu.Unlock()
			}
		}
	}()
}

// runSerialEndpoint opens a real/virtual serial port and serves RTU frames.
func runSerialEndpoint(ctx context.Context, ep Endpoint) error {
	// Optionally spawn socat to create a virtual serial pair
	var socatCmd *exec.Cmd
	if ep.SpawnSocat {
		link := ep.SocatLink
		peer := ep.SocatPeer
		if link == "" && ep.SerialPort != "" {
			link = ep.SerialPort
		}
		if link == "" || peer == "" {
			return fmt.Errorf("spawn_socat requires socat_link (or serial_port) and socat_peer")
		}
		// socat -d -d pty,raw,echo=0,link=link pty,raw,echo=0,link=peer
		socatCmd = utils.BuildSocatPairCmd(ctx, utils.SocatPair{Link: link, Peer: peer})
		socatCmd.Stdout = os.Stdout
		socatCmd.Stderr = os.Stderr
		if err := socatCmd.Start(); err != nil {
			return fmt.Errorf("start socat: %w", err)
		}
		log.Printf("mocktty: spawned socat pair link=%s peer=%s (pid=%d)", link, peer, socatCmd.Process.Pid)
		// Wait a moment for device creation
		time.Sleep(400 * time.Millisecond)
		// Ensure the serial open uses link path
		if ep.SerialPort == "" {
			ep.SerialPort = link
		}
	}
	// Configure and open serial via utils
	sp := utils.SerialParams{
		Address:  ep.SerialPort,
		BaudRate: ep.BaudRate,
		DataBits: ep.DataBits,
		StopBits: ep.StopBits,
		Parity:   ep.Parity,
		Timeout:  10 * time.Second,
	}
	rw, err := utils.OpenSerial(sp)
	if err != nil { return err }
	defer rw.Close()

	st := newStore()
	// seed demo values
	st.setHolding(100, 1); st.setHolding(101, 2); st.setHolding(102, 0xABCD)
	st.setInput(200, 0xCAFE)
	st.setCoil(0, true); st.setCoil(2, true); st.setCoil(3, true)

	stop := make(chan struct{})
	startDynamic(st, ep.UpdateInterval, stop)

	log.Printf("mocktty: %s listening (Serial) on %s slave=%d baud=%d data=%d stop=%d parity=%s",
		ep.Name, ep.SerialPort, ep.SlaveID, ep.BaudRate, ep.DataBits, ep.StopBits, ep.Parity)

	done := make(chan struct{})
	go func() { defer close(done); handleStream(rw, st, ep.SlaveID) }()

	<-ctx.Done()
	close(stop)
	rw.Close()
	if socatCmd != nil && socatCmd.Process != nil {
		_ = socatCmd.Process.Signal(syscall.SIGTERM)
		// Give it a grace period
		doneKill := make(chan struct{})
		go func() { _ = socatCmd.Wait(); close(doneKill) }()
		select {
		case <-doneKill:
		case <-time.After(2 * time.Second):
			_ = socatCmd.Process.Kill()
		}
	}
	<-done
	return nil
}

// --- Server runner ---
func runEndpoint(ctx context.Context, ep Endpoint) error {
	addr := ep.ListenAddress
	if addr == "" { addr = "127.0.0.1:5020" }
	l, err := net.Listen("tcp", addr)
	if err != nil { return err }
	defer l.Close()

	st := newStore()
	// seed demo values
	st.setHolding(100, 1); st.setHolding(101, 2); st.setHolding(102, 0xABCD)
	st.setInput(200, 0xCAFE)
	st.setCoil(0, true); st.setCoil(2, true); st.setCoil(3, true)

	stop := make(chan struct{})
	startDynamic(st, ep.UpdateInterval, stop)

	log.Printf("mocktty: %s listening (RTU-over-TCP) on %s slave=%d baud=%d data=%d stop=%d parity=%s",
		ep.Name, addr, ep.SlaveID, ep.BaudRate, ep.DataBits, ep.StopBits, ep.Parity)

	var wg sync.WaitGroup
	go func() {
		<-ctx.Done()
		close(stop)
		l.Close()
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			select { case <-ctx.Done(): break; default: }
			return nil
		}
		wg.Add(1)
		go func(c net.Conn) { defer wg.Done(); handleConn(c, st, ep.SlaveID) }(conn)
	}
	// wg.Wait() // unreachable
}

func runAll(ctx context.Context, cfg RootConfig) error {
	var wg sync.WaitGroup
	for _, ep := range cfg.Endpoints {
		if ep.SlaveID == 0 { continue }
		mode := strings.ToLower(strings.TrimSpace(ep.Mode))
		if mode == "serial" || (mode == "" && ep.SerialPort != "") {
			wg.Add(1)
			go func(e Endpoint) { defer wg.Done(); _ = runSerialEndpoint(ctx, e) }(ep)
			continue
		}
		if mode == "rtu_over_tcp" || (mode == "" && ep.ListenAddress != "") {
			wg.Add(1)
			go func(e Endpoint) { defer wg.Done(); _ = runEndpoint(ctx, e) }(ep)
			continue
		}
		// skip if neither configured
	}
	<-ctx.Done()
	wg.Wait()
	return nil
}

func main() {
	var cfgPath string
	flag.StringVar(&cfgPath, "config", "config/mocktty.yaml", "path to mocktty YAML config")
	flag.Parse()

	cfg, err := loadConfig(cfgPath)
	if err != nil { log.Fatalf("load config: %v", err) }
	if len(cfg.Endpoints) == 0 { log.Fatalf("config has no endpoints") }

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := runAll(ctx, cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

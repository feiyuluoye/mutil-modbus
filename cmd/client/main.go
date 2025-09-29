package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	cfgpkg "modbus-simulator/internal/config"

	mb "github.com/goburrow/modbus"
)

func main() {
	// Load config
	cfg, err := cfgpkg.Load("config.toml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	address := normalizeAddress(cfg.Server.ListenAddress)

	// Setup Modbus TCP client handler
	h := mb.NewTCPClientHandler(address)
	h.Timeout = 3 * time.Second
	h.SlaveId = 1 // not enforced by our server but set anyway

	if err := h.Connect(); err != nil {
		log.Fatalf("connect %s: %v", address, err)
	}
	defer h.Close()

	client := mb.NewClient(h)

	for _, r := range cfg.Registers {
		switch strings.ToLower(r.Type) {
		case "holding":
			data, err := client.ReadHoldingRegisters(r.Address, 1)
			if err != nil {
				log.Printf("read holding %d: %v", r.Address, err)
				continue
			}
			val := binary.BigEndian.Uint16(data)
			fmt.Printf("%s (holding@%d) = %d\n", r.CSVColumn, r.Address, val)

		case "input":
			data, err := client.ReadInputRegisters(r.Address, 1)
			if err != nil {
				log.Printf("read input %d: %v", r.Address, err)
				continue
			}
			val := binary.BigEndian.Uint16(data)
			fmt.Printf("%s (input@%d) = %d\n", r.CSVColumn, r.Address, val)

		case "coil":
			data, err := client.ReadCoils(r.Address, 1)
			if err != nil {
				log.Printf("read coil %d: %v", r.Address, err)
				continue
			}
			b := (len(data) > 0) && (data[0]&0x01 == 0x01)
			fmt.Printf("%s (coil@%d) = %t\n", r.CSVColumn, r.Address, b)

		case "discrete":
			data, err := client.ReadDiscreteInputs(r.Address, 1)
			if err != nil {
				log.Printf("read discrete %d: %v", r.Address, err)
				continue
			}
			b := (len(data) > 0) && (data[0]&0x01 == 0x01)
			fmt.Printf("%s (discrete@%d) = %t\n", r.CSVColumn, r.Address, b)

		default:
			log.Printf("unknown register type: %s", r.Type)
		}
	}
}

func normalizeAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = ":1503"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	// If it's just a port number like "1502", make it host:port
	if _, _, err := net.SplitHostPort(addr); err != nil {
		if !strings.Contains(addr, ":") {
			addr = "127.0.0.1:" + addr
		}
	}
	return addr
}

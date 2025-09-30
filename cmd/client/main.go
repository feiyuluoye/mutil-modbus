package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	cfgpkg "modbus-simulator/internal/config"

	mb "github.com/goburrow/modbus"
)

func main() {
	// Load config
	var configPath string
	flag.StringVar(&configPath, "config", "config.toml", "Path to configuration file")
	flag.Parse()

	cfg, err := cfgpkg.Load(configPath)
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
		typeLower := strings.ToLower(r.Type)
		switch typeLower {
		case "holding":
			value, err := readNumericRegister(client, r, true)
			if err != nil {
				log.Printf("read holding %d: %v", r.Address, err)
				continue
			}
			actual := applyScaleAndOffset(value, r)
			fmt.Printf("%s (holding@%d) = %s\n", r.CSVColumn, r.Address, formatNumber(actual))

		case "input":
			value, err := readNumericRegister(client, r, false)
			if err != nil {
				log.Printf("read input %d: %v", r.Address, err)
				continue
			}
			actual := applyScaleAndOffset(value, r)
			fmt.Printf("%s (input@%d) = %s\n", r.CSVColumn, r.Address, formatNumber(actual))

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

func readNumericRegister(client mb.Client, reg cfgpkg.RegisterConfig, holding bool) (float64, error) {
	dataType := strings.ToLower(reg.DataType)
	if dataType == "" {
		dataType = "uint16"
	}

	quantity := uint16(1)
	if dataType == "float32" {
		quantity = 2
	}

	var data []byte
	var err error
	if holding {
		data, err = client.ReadHoldingRegisters(reg.Address, quantity)
	} else {
		data, err = client.ReadInputRegisters(reg.Address, quantity)
	}
	if err != nil {
		return 0, err
	}

	switch dataType {
	case "uint16":
		return float64(binary.BigEndian.Uint16(data)), nil
	case "int16":
		return float64(int16(binary.BigEndian.Uint16(data))), nil
	case "float32":
		if len(data) < 4 {
			return 0, fmt.Errorf("float32 read returned %d bytes", len(data))
		}
		bits := binary.BigEndian.Uint32(data)
		return float64(math.Float32frombits(bits)), nil
	default:
		return 0, fmt.Errorf("unsupported data_type %q", reg.DataType)
	}
}

func applyScaleAndOffset(raw float64, reg cfgpkg.RegisterConfig) float64 {
	scale := reg.Scale
	if scale == 0 {
		scale = 1
	}
	return (raw - reg.Offset) / scale
}

func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
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

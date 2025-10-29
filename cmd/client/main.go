package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"math"
	"net"
	"os"
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

    // Choose settings: prefer [client] section if present, else fallback to [server]
    settings := cfg.Server
    if cfg.Client.Mode != "" || cfg.Client.SerialPort != "" || cfg.Client.ListenAddress != "" {
        settings = cfg.Client
    }
    // Build client handler from chosen settings
    mode := strings.ToLower(settings.Mode)
    timeout := 5 * time.Second
    sid := uint8(1)
    if settings.SlaveID > 0 {
        sid = uint8(settings.SlaveID)
    }

	var client mb.Client
    if mode == "rtu" || strings.TrimSpace(settings.SerialPort) != "" {
        port := strings.TrimSpace(settings.SerialPort)
        if port == "" {
            log.Fatalf("serial_port/path is required for RTU mode")
        }
        if _, err := os.Stat(port); err != nil {
            log.Fatalf("serial port %s not ready: %v", port, err)
        }
        rh := mb.NewRTUClientHandler(port)
        if settings.BaudRate > 0 { rh.BaudRate = settings.BaudRate }
        if settings.DataBits > 0 { rh.DataBits = settings.DataBits }
        if settings.StopBits > 0 { rh.StopBits = settings.StopBits }
        if p := strings.ToUpper(strings.TrimSpace(settings.Parity)); p != "" { rh.Parity = p }
        rh.Timeout = timeout
        rh.IdleTimeout = 100 * time.Millisecond
        rh.SlaveId = sid
        if err := rh.Connect(); err != nil { log.Fatalf("connect (rtu): %v", err) }
        defer rh.Close()
        client = mb.NewClient(rh)
    } else {
        address := normalizeAddress(settings.ListenAddress)
        th := mb.NewTCPClientHandler(address)
        th.Timeout = timeout
        th.SlaveId = sid
        if err := th.Connect(); err != nil { log.Fatalf("connect (tcp): %v", err) }
        defer th.Close()
        client = mb.NewClient(th)
    }

    // Determine poll interval: prefer [client].update_interval, then root, else 5s
    intervalStr := settings.UpdateInterval
    if intervalStr == "" {
        intervalStr = cfg.UpdateInterval
    }
    poll := 5 * time.Second
    if d, err := time.ParseDuration(strings.TrimSpace(intervalStr)); err == nil && d > 0 {
        poll = d
    }

    ticker := time.NewTicker(poll)
    defer ticker.Stop()

    for {
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
        <-ticker.C
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

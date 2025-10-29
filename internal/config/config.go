package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Server         ServerSettings
	Client         ServerSettings
	Registers      []RegisterConfig
	CSVFile        string
	UpdateInterval string
}

type ServerSettings struct {
	ListenAddress string
	Mode          string // "tcp" or "rtu"
	SerialPort    string
	BaudRate      int
	DataBits      int
	StopBits      int
	Parity        string // N,E,O
	// Optional fields tolerated by parser (not necessarily used everywhere)
	SlaveID        int
	UpdateInterval string
}

type RegisterConfig struct {
	Type      string
	Address   uint16
	CSVColumn string
	Scale     float64
	Offset    float64
	DataType  string
}

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()

	cfg := Config{}
	currentSection := ""
	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.Trim(line, "[]")
			if strings.HasPrefix(section, "[") && strings.HasSuffix(section, "]") {
				section = strings.Trim(section, "[]")
			}
			if section == "registers" && strings.HasPrefix(line, "[[") {
				cfg.Registers = append(cfg.Registers, RegisterConfig{Scale: 1})
				currentSection = "registers"
			} else if section == "server" {
				currentSection = "server"
			} else if section == "client" {
				currentSection = "client"
			} else {
				return Config{}, fmt.Errorf("unsupported section %s on line %d", section, lineNum)
			}
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return Config{}, fmt.Errorf("invalid line %d: %s", lineNum, line)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch currentSection {
		case "":
			if err := assignRoot(&cfg, key, value); err != nil {
				return Config{}, fmt.Errorf("line %d: %w", lineNum, err)
			}
		case "server":
			if err := assignServer(&cfg.Server, key, value); err != nil {
				return Config{}, fmt.Errorf("line %d: %w", lineNum, err)
			}
		case "client":
			if err := assignServer(&cfg.Client, key, value); err != nil {
				return Config{}, fmt.Errorf("line %d: %w", lineNum, err)
			}
		case "registers":
			if len(cfg.Registers) == 0 {
				return Config{}, errors.New("register entry defined before [[registers]] header")
			}
			idx := len(cfg.Registers) - 1
			if err := assignRegister(&cfg.Registers[idx], key, value); err != nil {
				return Config{}, fmt.Errorf("line %d: %w", lineNum, err)
			}
		default:
			return Config{}, fmt.Errorf("unknown section at line %d", lineNum)
		}
	}
	if err := scanner.Err(); err != nil {
		return Config{}, err
	}

	if cfg.Server.ListenAddress == "" {
		cfg.Server.ListenAddress = ":1502"
	}
	if cfg.UpdateInterval == "" {
		cfg.UpdateInterval = "5s"
	}
	if cfg.CSVFile == "" {
		return Config{}, errors.New("csv_file must be set")
	}
	if len(cfg.Registers) == 0 {
		return Config{}, errors.New("at least one register must be configured")
	}

	return cfg, nil
}

func assignRoot(cfg *Config, key, value string) error {
	switch key {
	case "csv_file":
		cfg.CSVFile = parseString(value)
	case "update_interval":
		cfg.UpdateInterval = parseString(value)
	default:
		return fmt.Errorf("unknown key %s", key)
	}
	return nil
}

func assignServer(server *ServerSettings, key, value string) error {
	switch key {
	case "listen_address":
		server.ListenAddress = parseString(value)
	case "mode":
		server.Mode = strings.ToLower(parseString(value))
	case "serial_port":
		server.SerialPort = parseString(value)
	case "path":
		// accept "path" as alias for serial_port
		server.SerialPort = parseString(value)
	case "baud_rate":
		v, err := strconv.ParseInt(parseString(value), 10, 32)
		if err != nil {
			return fmt.Errorf("invalid baud_rate: %w", err)
		}
		server.BaudRate = int(v)
	case "data_bits":
		v, err := strconv.ParseInt(parseString(value), 10, 32)
		if err != nil {
			return fmt.Errorf("invalid data_bits: %w", err)
		}
		server.DataBits = int(v)
	case "stop_bits":
		v, err := strconv.ParseInt(parseString(value), 10, 32)
		if err != nil {
			return fmt.Errorf("invalid stop_bits: %w", err)
		}
		server.StopBits = int(v)
	case "parity":
		server.Parity = strings.ToUpper(parseString(value))
	case "slave_id":
		v, err := strconv.ParseInt(parseString(value), 10, 32)
		if err != nil {
			return fmt.Errorf("invalid slave_id: %w", err)
		}
		server.SlaveID = int(v)
	case "update_interval":
		server.UpdateInterval = parseString(value)
	default:
		return fmt.Errorf("unknown server key %s", key)
	}
	return nil
}

func assignRegister(reg *RegisterConfig, key, value string) error {
	switch key {
	case "type":
		reg.Type = strings.ToLower(parseString(value))
	case "address":
		v, err := strconv.ParseUint(parseString(value), 10, 16)
		if err != nil {
			return fmt.Errorf("invalid address value: %w", err)
		}
		reg.Address = uint16(v)
	case "csv_column":
		reg.CSVColumn = parseString(value)
	case "scale":
		parsed, err := strconv.ParseFloat(parseString(value), 64)
		if err != nil {
			return fmt.Errorf("invalid scale value: %w", err)
		}
		reg.Scale = parsed
	case "offset":
		parsed, err := strconv.ParseFloat(parseString(value), 64)
		if err != nil {
			return fmt.Errorf("invalid offset value: %w", err)
		}
		reg.Offset = parsed
	case "data_type":
		reg.DataType = strings.ToLower(parseString(value))
	default:
		return fmt.Errorf("unknown register key %s", key)
	}
	return nil
}

func parseString(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
		return strings.Trim(value, "\"")
	}
	return value
}

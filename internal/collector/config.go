package collector

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Root configuration for the concurrent collector manager.
// This mirrors config/config.yaml.

type RootConfig struct {
	System    SystemConfig             `yaml:"system"`
	Frequency map[string]time.Duration `yaml:"frequency"`
	Servers   []ServerConfig           `yaml:"servers"`
}

type SystemConfig struct {
	Processing struct {
		Enabled      bool `yaml:"enabled"`
		MaxWorkers   int  `yaml:"max_workers"`
		MaxQueueSize int  `yaml:"max_queue_size"`
	} `yaml:"processing"`
	Storage struct {
		Enabled      bool   `yaml:"enabled"`
		FileType     string `yaml:"file_type"`
		DBPath       string `yaml:"db_path"`
		MaxWorkers   int    `yaml:"max_workers"`
		MaxQueueSize int    `yaml:"max_queue_size"`
	} `yaml:"storage"`
}

type ServerConfig struct {
	ServerID   string        `yaml:"server_id"`
	ServerName string        `yaml:"server_name"`
	Protocol   string        `yaml:"protocol"` // modbus-tcp | modbus-rtu
	Connection Connection    `yaml:"connection"`
	Timeout    time.Duration `yaml:"timeout"`
	RetryCount int           `yaml:"retry_count"`
	Enabled    bool          `yaml:"enabled"`
	DevicesType string       `yaml:"type"`
	DevicesFile string       `yaml:"devices_file"`
	Devices    []Device      `yaml:"devices"`
}

type Connection struct {
	// TCP
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
	// RTU (not yet implemented)
	SerialPort string `yaml:"serial_port"`
	BaudRate   int    `yaml:"baud_rate"`
	DataBits   int    `yaml:"data_bits"`
	StopBits   int    `yaml:"stop_bits"`
	Parity     string `yaml:"parity"`
}

type Device struct {
	DeviceID     string        `yaml:"device_id"`
	Vendor       string        `yaml:"vendor"`
	SlaveID      uint8         `yaml:"slave_id"`
	PollInterval time.Duration `yaml:"poll_interval"`
	Points       []Point       `yaml:"points"`
}

type Point struct {
	Address      uint16  `yaml:"address"`
	Name         string  `yaml:"name"`
	DataType     string  `yaml:"data_type"`     // uint16 | float32
	ByteOrder    string  `yaml:"byte_order"`    // ABCD (big-endian) supported initially
	RegisterType string  `yaml:"register_type"` // holding | input | coil | discrete (read-only)
	Scale        float64 `yaml:"scale"`
	Offset       float64 `yaml:"offset"`
	Unit         string  `yaml:"unit"`
}

func LoadYAML(path string) (RootConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return RootConfig{}, err
	}
	var cfg RootConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return RootConfig{}, err
	}
	// Defaults
	if cfg.System.Processing.MaxWorkers <= 0 {
		cfg.System.Processing.MaxWorkers = 10
	}
	if cfg.System.Processing.MaxQueueSize <= 0 {
		cfg.System.Processing.MaxQueueSize = 1000
	}
	if cfg.System.Storage.MaxWorkers < 0 {
		cfg.System.Storage.MaxWorkers = 0
	}
	if cfg.System.Storage.MaxQueueSize < 0 {
		cfg.System.Storage.MaxQueueSize = 0
	}
	cfg.System.Storage.FileType = strings.ToLower(strings.TrimSpace(cfg.System.Storage.FileType))
	if cfg.System.Storage.FileType == "" {
		cfg.System.Storage.FileType = "csv"
	}

	cfgDir := filepath.Dir(path)
	for i := range cfg.Servers {
		srv := &cfg.Servers[i]
		srcType := strings.ToLower(strings.TrimSpace(srv.DevicesType))
		switch srcType {
		case "", "device", "devices", "points":
			srv.DevicesType = "device"
			if len(srv.Devices) == 0 {
				return RootConfig{}, fmt.Errorf("server %s: devices list is empty", srv.ServerID)
			}
		case "csvfile", "csv":
			if strings.TrimSpace(srv.DevicesFile) == "" {
				return RootConfig{}, fmt.Errorf("server %s: devices_file is required for csvfile type", srv.ServerID)
			}
			csvPath := srv.DevicesFile
			if !filepath.IsAbs(csvPath) {
				csvPath = filepath.Join(cfgDir, csvPath)
			}
			devices, err := loadDevicesFromCSV(csvPath)
			if err != nil {
				return RootConfig{}, fmt.Errorf("server %s: %w", srv.ServerID, err)
			}
			srv.Devices = devices
			srv.DevicesFile = csvPath
			srv.DevicesType = "csvfile"
		default:
			return RootConfig{}, fmt.Errorf("server %s: unsupported devices type %q", srv.ServerID, srv.DevicesType)
		}
	}
	if len(cfg.Servers) == 0 {
		return RootConfig{}, fmt.Errorf("no servers configured")
	}
	return cfg, nil
}

func loadDevicesFromCSV(path string) ([]Device, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open devices csv %s: %w", path, err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.TrimLeadingSpace = true
	header, err := r.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("devices csv %s: empty file", path)
		}
		return nil, fmt.Errorf("devices csv %s: read header: %w", path, err)
	}
	if len(header) == 0 {
		return nil, fmt.Errorf("devices csv %s: empty header", path)
	}

	index := make(map[string]int, len(header))
	for i, col := range header {
		index[strings.ToLower(strings.TrimSpace(col))] = i
	}

	required := []string{"device_id", "address", "register_type"}
	for _, key := range required {
		if _, ok := index[key]; !ok {
			return nil, fmt.Errorf("devices csv %s: missing required column %q", path, key)
		}
	}

	deviceMap := make(map[string]*Device)
	order := make([]string, 0)

	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("devices csv %s: read row: %w", path, err)
		}
		trim := func(key string) string {
			idx, ok := index[key]
			if !ok || idx >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[idx])
		}

		deviceID := trim("device_id")
		if deviceID == "" {
			return nil, fmt.Errorf("devices csv %s: row without device_id", path)
		}

		dev := deviceMap[deviceID]
		if dev == nil {
			slaveStr := trim("slave_id")
			var slaveVal uint64
			if slaveStr != "" {
				slaveVal, err = strconv.ParseUint(slaveStr, 10, 8)
				if err != nil {
					return nil, fmt.Errorf("devices csv %s: device %s invalid slave_id", path, deviceID)
				}
			}
			poll := trim("poll_interval")
			var pollDur time.Duration
			if poll != "" {
				pollDur, err = time.ParseDuration(poll)
				if err != nil {
					return nil, fmt.Errorf("devices csv %s: device %s invalid poll_interval", path, deviceID)
				}
			}
			dev = &Device{
				DeviceID:     deviceID,
				Vendor:       trim("vendor"),
				SlaveID:      uint8(slaveVal),
				PollInterval: pollDur,
			}
			if dev.PollInterval <= 0 {
				dev.PollInterval = 5 * time.Second
			}
			deviceMap[deviceID] = dev
			order = append(order, deviceID)
		}

		pointName := trim("point_name")
		if pointName == "" {
			pointName = trim("name")
		}
		if pointName == "" {
			return nil, fmt.Errorf("devices csv %s: device %s point without name", path, deviceID)
		}

		addrStr := trim("address")
		addrVal, err := strconv.ParseUint(addrStr, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("devices csv %s: device %s point %s invalid address", path, deviceID, pointName)
		}

		registerType := trim("register_type")
		if registerType == "" {
			return nil, fmt.Errorf("devices csv %s: device %s point %s missing register_type", path, deviceID, pointName)
		}

		scale := 1.0
		if val := trim("scale"); val != "" {
			scale, err = strconv.ParseFloat(val, 64)
			if err != nil {
				return nil, fmt.Errorf("devices csv %s: device %s point %s invalid scale", path, deviceID, pointName)
			}
		}

		offset := 0.0
		if val := trim("offset"); val != "" {
			offset, err = strconv.ParseFloat(val, 64)
			if err != nil {
				return nil, fmt.Errorf("devices csv %s: device %s point %s invalid offset", path, deviceID, pointName)
			}
		}

		dev.Points = append(dev.Points, Point{
			Address:      uint16(addrVal),
			Name:         pointName,
			DataType:     trim("data_type"),
			ByteOrder:    trim("byte_order"),
			RegisterType: registerType,
			Scale:        scale,
			Offset:       offset,
			Unit:         trim("unit"),
		})
	}

	if len(deviceMap) == 0 {
		return nil, fmt.Errorf("devices csv %s: no device rows", path)
	}

	devices := make([]Device, 0, len(order))
	for _, id := range order {
		dev := deviceMap[id]
		if len(dev.Points) == 0 {
			return nil, fmt.Errorf("devices csv %s: device %s has no points", path, id)
		}
		devices = append(devices, *dev)
	}
	return devices, nil
}

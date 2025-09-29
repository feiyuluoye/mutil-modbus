package collector

import (
	"fmt"
	"os"
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
	// Basic validation
	if len(cfg.Servers) == 0 {
		return RootConfig{}, fmt.Errorf("no servers configured")
	}
	return cfg, nil
}

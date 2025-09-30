package model

import "time"

// Server represents a Modbus server definition.
type Server struct {
	ServerID     string `gorm:"column:server_id;primaryKey"`
	ServerName   string `gorm:"column:server_name"`
	Protocol     string `gorm:"column:protocol"`
	Host         string `gorm:"column:host"`
	Port         int    `gorm:"column:port"`
	Timeout      string `gorm:"column:timeout"`
	RetryCount   int    `gorm:"column:retry_count"`
	Enabled      bool   `gorm:"column:enabled"`
	PollInterval string `gorm:"column:poll_interval"`

	Devices []Device `gorm:"foreignKey:ServerID;references:ServerID"`
}

func (Server) TableName() string { return "servers" }

// Device represents a Modbus device belonging to a server.
type Device struct {
	DeviceID     string `gorm:"column:device_id;primaryKey"`
	ServerID     string `gorm:"column:server_id;index"`
	Vendor       string `gorm:"column:vendor"`
	SlaveID      int    `gorm:"column:slave_id"`
	PollInterval string `gorm:"column:poll_interval"`

	Server      Server       `gorm:"foreignKey:ServerID;references:ServerID"`
	PointValues []PointValue `gorm:"foreignKey:DeviceID;references:DeviceID"`
}

func (Device) TableName() string { return "devices" }

// PointValue captures the value of a point.
type PointValue struct {
	ID           uint      `gorm:"column:id;primaryKey;autoIncrement"`
	DeviceID     string    `gorm:"column:device_id;index"`
	Name         string    `gorm:"column:name"`
	Address      int       `gorm:"column:address"`
	RegisterType string    `gorm:"column:register_type"`
	DataType     string    `gorm:"column:data_type"`
	ByteOrder    string    `gorm:"column:byte_order"`
	Scale        float64   `gorm:"column:scale;default:1"`
	Offset       float64   `gorm:"column:offset;default:0"`
	Unit         string    `gorm:"column:unit"`
	Value        float64   `gorm:"column:value"`
	Timestamp    time.Time `gorm:"column:timestamp;autoCreateTime"`

	Device Device `gorm:"foreignKey:DeviceID;references:DeviceID"`
}

func (PointValue) TableName() string { return "point_values" }

package model

import "time"

// PointSnapshot represents a single point's current value
// Only one of ValueUint16 or ValueBool will be set depending on register type.
type PointSnapshot struct {
	Name         string    `json:"name"`
	RegisterType string    `json:"register_type"`
	Address      uint16    `json:"address"`
	Unit         string    `json:"unit"`
	ValueUint16  *uint16   `json:"value_uint16,omitempty"`
	ValueBool    *bool     `json:"value_bool,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
}

// DeviceSnapshot aggregates all points for a device.
type DeviceSnapshot struct {
	DeviceID string           `json:"device_id"`
	Vendor   string           `json:"vendor"`
	SlaveID  uint8            `json:"slave_id"`
	Points   []PointSnapshot  `json:"points"`
}

// ServerSnapshot aggregates all devices of a server.
type ServerSnapshot struct {
	ServerID   string            `json:"server_id"`
	ServerName string            `json:"server_name"`
	Address    string            `json:"address"`
	Devices    []DeviceSnapshot  `json:"devices"`
	Timestamp  time.Time         `json:"timestamp"`
}

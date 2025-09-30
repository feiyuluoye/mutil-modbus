package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps sqlite connection
type DB struct {
	SQL *sql.DB
}

// DevicePointsWithLimit returns latest point_values rows for a device limited by count.
func (d *DB) DevicePointsWithLimit(ctx context.Context, deviceID string, limit int) ([]DevicePoint, error) {
	if limit <= 0 {
		return d.DevicePoints(ctx, deviceID)
	}
	q := `
SELECT device_id, name, address, register_type, data_type, byte_order, unit, COALESCE(value, 0.0), timestamp
FROM point_values
WHERE device_id = ?
ORDER BY timestamp DESC, name
LIMIT ?;
`
	rows, err := d.SQL.QueryContext(ctx, q, deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DevicePoint
	for rows.Next() {
		var p DevicePoint
		if err := rows.Scan(&p.DeviceID, &p.Name, &p.Address, &p.RegisterType, &p.DataType, &p.ByteOrder, &p.Unit, &p.Value, &p.Timestamp); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ServerInfo mirrors a subset of the servers table for stats output
type ServerInfo struct {
	ServerID   string `json:"server_id"`
	ServerName string `json:"server_name"`
	Protocol   string `json:"protocol"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
}

// DeviceInfo mirrors a subset of the devices table for stats output
type DeviceInfo struct {
	DeviceID     string `json:"device_id"`
	ServerID     string `json:"server_id"`
	Vendor       string `json:"vendor"`
	SlaveID      int    `json:"slave_id"`
	PollInterval string `json:"poll_interval"`
}

// DevicePoint mirrors a row from point_values for latest snapshot style output
type DevicePoint struct {
	DeviceID     string    `json:"device_id"`
	Name         string    `json:"name"`
	Address      int       `json:"address"`
	RegisterType string    `json:"register_type"`
	DataType     string    `json:"data_type"`
	ByteOrder    string    `json:"byte_order"`
	Unit         string    `json:"unit"`
	Value        float64   `json:"value"`
	Timestamp    time.Time `json:"timestamp"`
}

// ListServers returns all servers
func (d *DB) ListServers(ctx context.Context) ([]ServerInfo, error) {
	const q = `SELECT server_id, server_name, protocol, host, port FROM servers ORDER BY server_id`
	rows, err := d.SQL.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ServerInfo
	for rows.Next() {
		var s ServerInfo
		if err := rows.Scan(&s.ServerID, &s.ServerName, &s.Protocol, &s.Host, &s.Port); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListDevices returns all devices
func (d *DB) ListDevices(ctx context.Context) ([]DeviceInfo, error) {
	const q = `SELECT device_id, server_id, vendor, slave_id, poll_interval FROM devices ORDER BY device_id`
	rows, err := d.SQL.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeviceInfo
	for rows.Next() {
		var di DeviceInfo
		if err := rows.Scan(&di.DeviceID, &di.ServerID, &di.Vendor, &di.SlaveID, &di.PollInterval); err != nil {
			return nil, err
		}
		out = append(out, di)
	}
	return out, rows.Err()
}

// DevicePoints returns all point_values rows for a device.
// If you want latest values per point, select the max(timestamp) per (device_id,name).
func (d *DB) DevicePoints(ctx context.Context, deviceID string) ([]DevicePoint, error) {
	const q = `
SELECT device_id, name, address, register_type, data_type, byte_order, unit, COALESCE(value, 0.0), timestamp
FROM point_values
WHERE device_id = ?
ORDER BY timestamp DESC, name;
`
	rows, err := d.SQL.QueryContext(ctx, q, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DevicePoint
	for rows.Next() {
		var p DevicePoint
		if err := rows.Scan(&p.DeviceID, &p.Name, &p.Address, &p.RegisterType, &p.DataType, &p.ByteOrder, &p.Unit, &p.Value, &p.Timestamp); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Stats aggregates server/device lists and device points for a given deviceID
type Stats struct {
	ServerCount       int           `json:"server_count"`
	Servers           []ServerInfo  `json:"servers"`
	DeviceCount       int           `json:"device_count"`
	Devices           []DeviceInfo  `json:"devices"`
	DevicePointsCount int           `json:"device_points_count"`
	DevicePoints      []DevicePoint `json:"device_points"`
}

// StatsJSON returns aggregated stats in JSON for a given deviceID
func (d *DB) StatsJSON(ctx context.Context, deviceID string) ([]byte, error) {
	return d.StatsJSONWithLimit(ctx, deviceID, 0)
}

// StatsJSONWithLimit works like StatsJSON but limits number of device points returned when limit > 0.
func (d *DB) StatsJSONWithLimit(ctx context.Context, deviceID string, limit int) ([]byte, error) {
	servers, err := d.ListServers(ctx)
	if err != nil {
		return nil, err
	}
	devices, err := d.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	var points []DevicePoint
	if limit > 0 {
		points, err = d.DevicePointsWithLimit(ctx, deviceID, limit)
	} else {
		points, err = d.DevicePoints(ctx, deviceID)
	}
	if err != nil {
		return nil, err
	}
	st := Stats{
		ServerCount:       len(servers),
		Servers:           servers,
		DeviceCount:       len(devices),
		Devices:           devices,
		DevicePointsCount: len(points),
		DevicePoints:      points,
	}
	return json.Marshal(st)
}

// PointLatest represents the latest record for each unique point name across all devices.
type PointLatest struct {
	DeviceID     string    `json:"device_id"`
	Name         string    `json:"name"`
	Address      int       `json:"address"`
	RegisterType string    `json:"register_type"`
	DataType     string    `json:"data_type"`
	ByteOrder    string    `json:"byte_order"`
	Unit         string    `json:"unit"`
	Value        float64   `json:"value"`
	Timestamp    time.Time `json:"timestamp"`
}

// LatestPoints returns, for each unique point name, the latest row by timestamp.
func (d *DB) LatestPoints(ctx context.Context) ([]PointLatest, error) {
	const q = `
WITH latest AS (
  SELECT name, MAX(timestamp) AS ts
  FROM point_values
  GROUP BY name
)
SELECT p.device_id, p.name, p.address, p.register_type, p.data_type, p.byte_order, p.unit, COALESCE(p.value, 0.0), p.timestamp
FROM point_values p
JOIN latest l ON l.name = p.name AND l.ts = p.timestamp
ORDER BY p.name;
`
	rows, err := d.SQL.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PointLatest
	for rows.Next() {
		var pl PointLatest
		if err := rows.Scan(&pl.DeviceID, &pl.Name, &pl.Address, &pl.RegisterType, &pl.DataType, &pl.ByteOrder, &pl.Unit, &pl.Value, &pl.Timestamp); err != nil {
			return nil, err
		}
		out = append(out, pl)
	}
	return out, rows.Err()
}

// LatestPointsJSON returns a JSON array of latest values keyed by unique point name.
func (d *DB) LatestPointsJSON(ctx context.Context) ([]byte, error) {
	pts, err := d.LatestPoints(ctx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(pts)
}

func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(ON)", path)
	s, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := s.Ping(); err != nil {
		s.Close()
		return nil, err
	}
	d := &DB{SQL: s}
	if err := d.migrate(); err != nil {
		s.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() error { return d.SQL.Close() }

func (d *DB) migrate() error {
	schema := `
CREATE TABLE IF NOT EXISTS servers (
    server_id TEXT PRIMARY KEY,
    server_name TEXT NOT NULL,
    protocol TEXT NOT NULL,
    host TEXT NOT NULL,
    port INTEGER NOT NULL,
    timeout TEXT,
    retry_count INTEGER,
    enabled BOOLEAN NOT NULL DEFAULT 1,
    poll_interval TEXT
);
CREATE TABLE IF NOT EXISTS devices (
    device_id TEXT PRIMARY KEY,
    server_id TEXT NOT NULL,
    vendor TEXT,
    slave_id INTEGER,
    poll_interval TEXT,
    FOREIGN KEY (server_id) REFERENCES servers(server_id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS point_values (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id TEXT NOT NULL,
    name TEXT NOT NULL,
    address INTEGER NOT NULL,
    register_type TEXT NOT NULL,
    data_type TEXT NOT NULL,
    byte_order TEXT NOT NULL,
    scale REAL NOT NULL DEFAULT 1.0,
    offset REAL NOT NULL DEFAULT 0.0,
    unit TEXT,
    value REAL,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (device_id) REFERENCES devices(device_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_point_values_device_id ON point_values(device_id);
CREATE INDEX IF NOT EXISTS idx_point_values_timestamp ON point_values(timestamp);
CREATE INDEX IF NOT EXISTS idx_devices_server_id ON devices(server_id);
`
	_, err := d.SQL.Exec(schema)
	return err
}

package db

import (
	"context"
	"encoding/json"
	"time"

	"modbus-simulator/internal/model"

	"gorm.io/gorm"
)

// DB wraps sqlite connection
type DB struct {
	ORM *gorm.DB
}

// ListDevices returns all devices
func (d *DB) ListDevices(ctx context.Context) ([]DeviceInfo, error) {
	var devs []model.Device
	if err := d.ORM.WithContext(ctx).Order("device_id").Find(&devs).Error; err != nil {
		return nil, err
	}
	out := make([]DeviceInfo, 0, len(devs))
	for _, di := range devs {
		out = append(out, DeviceInfo{DeviceID: di.DeviceID, ServerID: di.ServerID, Vendor: di.Vendor, SlaveID: di.SlaveID, PollInterval: di.PollInterval})
	}
	return out, nil
}

// DevicePoints returns all point_values rows for a device.
// If you want latest values per point, select the max(timestamp) per (device_id,name).
func (d *DB) DevicePoints(ctx context.Context, deviceID string) ([]DevicePoint, error) {
	var rows []model.PointValue
	if err := d.ORM.WithContext(ctx).
		Where("device_id = ?", deviceID).
		Order("timestamp DESC, name").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]DevicePoint, 0, len(rows))
	for _, r := range rows {
		out = append(out, DevicePoint{
			DeviceID:     r.DeviceID,
			Name:         r.Name,
			Address:      r.Address,
			RegisterType: r.RegisterType,
			DataType:     r.DataType,
			ByteOrder:    r.ByteOrder,
			Unit:         r.Unit,
			Value:        r.Value,
			Timestamp:    r.Timestamp,
		})
	}
	return out, nil
}

// DevicePointsWithLimit returns latest point_values rows for a device limited by count.
func (d *DB) DevicePointsWithLimit(ctx context.Context, deviceID string, limit int) ([]DevicePoint, error) {
	if limit <= 0 {
		return d.DevicePoints(ctx, deviceID)
	}
	var rows []model.PointValue
	if err := d.ORM.WithContext(ctx).
		Where("device_id = ?", deviceID).
		Order("timestamp DESC, name").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]DevicePoint, 0, len(rows))
	for _, r := range rows {
		out = append(out, DevicePoint{
			DeviceID:     r.DeviceID,
			Name:         r.Name,
			Address:      r.Address,
			RegisterType: r.RegisterType,
			DataType:     r.DataType,
			ByteOrder:    r.ByteOrder,
			Unit:         r.Unit,
			Value:        r.Value,
			Timestamp:    r.Timestamp,
		})
	}
	return out, nil
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
	var servers []model.Server
	if err := d.ORM.WithContext(ctx).Order("server_id").Find(&servers).Error; err != nil {
		return nil, err
	}
	out := make([]ServerInfo, 0, len(servers))
	for _, s := range servers {
		out = append(out, ServerInfo{ServerID: s.ServerID, ServerName: s.ServerName, Protocol: s.Protocol, Host: s.Host, Port: s.Port})
	}
	return out, nil
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

// PointLatest represents the latest record for each unique point name across all devices.
type PointLatest struct {
	ServerID     string    `json:"server_id"`
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
	// subquery: latest timestamp per unique (server_id, device_id, name)
	sub := d.ORM.Table("point_values as p").
		Joins("JOIN devices d ON d.device_id = p.device_id").
		Select("d.server_id as server_id, p.device_id as device_id, p.name as name, MAX(p.timestamp) as ts").
		Group("d.server_id, p.device_id, p.name")
	var out []PointLatest
	err := d.ORM.WithContext(ctx).
		Table("point_values as p").
		Select("d.server_id, p.device_id, p.name, p.address, p.register_type, p.data_type, p.byte_order, p.unit, COALESCE(p.value, 0.0) as value, p.timestamp").
		Joins("JOIN (?) as l ON l.server_id = d.server_id AND l.device_id = p.device_id AND l.name = p.name AND l.ts = p.timestamp", sub).
		Joins("JOIN devices d ON d.device_id = p.device_id").
		Order("p.name").
		Scan(&out).Error
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (d *DB) Close() error { return closeORM(d.ORM) }

// SavePointValue inserts a row into point_values via ORM.
func (d *DB) SavePointValue(ctx context.Context, pv *model.PointValue) error {
	return insertPointValue(ctx, d.ORM, pv)
}

// Open opens the SQLite database using GORM and runs migrations.
func Open(path string) (*DB, error) {
	g, err := openORM(path)
	if err != nil {
		return nil, err
	}
	if err := migrateORM(g); err != nil {
		_ = closeORM(g)
		return nil, err
	}
	return &DB{ORM: g}, nil
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

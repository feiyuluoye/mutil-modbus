package modbusdb

import (
	"context"
	"time"

	dbpkg "modbus-simulator/internal/db"
	"modbus-simulator/internal/model"
)

// --------------------
// Point DTOs
// --------------------

type PointValue struct {
	ID           uint
	DeviceID     string
	Name         string
	Address      int
	RegisterType string
	DataType     string
	ByteOrder    string
	Scale        float64
	Offset       float64
	Unit         string
	Value        float64
	Timestamp    time.Time
}

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

// --------------------
// Converters
// --------------------

func fromModelPointLatest(pl dbpkg.PointLatest) PointLatest {
	return PointLatest{
		ServerID:     pl.ServerID,
		DeviceID:     pl.DeviceID,
		Name:         pl.Name,
		Address:      pl.Address,
		RegisterType: pl.RegisterType,
		DataType:     pl.DataType,
		ByteOrder:    pl.ByteOrder,
		Unit:         pl.Unit,
		Value:        pl.Value,
		Timestamp:    pl.Timestamp,
	}
}

func fromModelPointValue(p model.PointValue) PointValue {
	return PointValue{
		ID:           p.ID,
		DeviceID:     p.DeviceID,
		Name:         p.Name,
		Address:      p.Address,
		RegisterType: p.RegisterType,
		DataType:     p.DataType,
		ByteOrder:    p.ByteOrder,
		Scale:        p.Scale,
		Offset:       p.Offset,
		Unit:         p.Unit,
		Value:        p.Value,
		Timestamp:    p.Timestamp,
	}
}

// --------------------
// Point operations
// --------------------

func (c *Client) SavePointValue(ctx context.Context, p *PointValue) error {
	mp := model.PointValue{
		ID:           p.ID,
		DeviceID:     p.DeviceID,
		Name:         p.Name,
		Address:      p.Address,
		RegisterType: p.RegisterType,
		DataType:     p.DataType,
		ByteOrder:    p.ByteOrder,
		Scale:        p.Scale,
		Offset:       p.Offset,
		Unit:         p.Unit,
		Value:        p.Value,
		Timestamp:    p.Timestamp,
	}
	return c.db.SavePointValue(ctx, &mp)
}

func (c *Client) SavePointValuesBatch(ctx context.Context, ps []PointValue, batchSize int) error {
	arr := make([]model.PointValue, 0, len(ps))
	for _, p := range ps {
		arr = append(arr, model.PointValue{
			ID:           p.ID,
			DeviceID:     p.DeviceID,
			Name:         p.Name,
			Address:      p.Address,
			RegisterType: p.RegisterType,
			DataType:     p.DataType,
			ByteOrder:    p.ByteOrder,
			Scale:        p.Scale,
			Offset:       p.Offset,
			Unit:         p.Unit,
			Value:        p.Value,
			Timestamp:    p.Timestamp,
		})
	}
	return dbpkg.InsertPointValuesBatch(ctx, c.db.ORM, arr, batchSize)
}

// Latest points with optional filters (serverID/deviceID)
func (c *Client) LatestPoints(ctx context.Context, serverID, deviceID string) ([]PointLatest, error) {
	pls, err := dbpkg.LatestPointsORM(ctx, c.db.ORM, serverID, deviceID)
	if err != nil {
		return nil, err
	}
	out := make([]PointLatest, 0, len(pls))
	for _, pl := range pls {
		out = append(out, fromModelPointLatest(pl))
	}
	return out, nil
}

// LatestPointsAll is a convenience wrapper for LatestPoints(ctx, "", "")
func (c *Client) LatestPointsAll(ctx context.Context) ([]PointLatest, error) {
	return c.LatestPoints(ctx, "", "")
}

// DeviceHistory returns historical point values for a device ordered by timestamp desc (and name).
// When limit > 0, returns at most 'limit' records.
func (c *Client) DeviceHistory(ctx context.Context, deviceID string, limit int) ([]PointValue, error) {
	rows, err := dbpkg.ListDevicePointValues(ctx, c.db.ORM, deviceID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]PointValue, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromModelPointValue(r))
	}
	return out, nil
}

// StatsJSON returns aggregated servers/devices/points as JSON.
// If limit > 0, device points will be limited to at most 'limit' items.
func (c *Client) StatsJSON(ctx context.Context, deviceID string, limit int) ([]byte, error) {
	return c.db.StatsJSONWithLimit(ctx, deviceID, limit)
}

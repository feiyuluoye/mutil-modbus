package modbusdb

import (
	"context"

	dbpkg "modbus-simulator/internal/db"
	"modbus-simulator/internal/model"
)

// --------------------
// Device DTOs and converters
// --------------------

type Device struct {
	DeviceID     string
	ServerID     string
	Vendor       string
	SlaveID      int
	PollInterval string
}

func toModelDevice(d *Device) *model.Device {
	if d == nil {
		return nil
	}
	return &model.Device{
		DeviceID:     d.DeviceID,
		ServerID:     d.ServerID,
		Vendor:       d.Vendor,
		SlaveID:      d.SlaveID,
		PollInterval: d.PollInterval,
	}
}

func fromModelDevice(d *model.Device) *Device {
	if d == nil {
		return nil
	}
	return &Device{
		DeviceID:     d.DeviceID,
		ServerID:     d.ServerID,
		Vendor:       d.Vendor,
		SlaveID:      d.SlaveID,
		PollInterval: d.PollInterval,
	}
}

// --------------------
// Device management (CRUD)
// --------------------

func (c *Client) CreateDevice(ctx context.Context, d *Device) error {
	return dbpkg.CreateDevice(ctx, c.db.ORM, toModelDevice(d))
}

func (c *Client) GetDevice(ctx context.Context, deviceID string) (*Device, error) {
	dev, err := dbpkg.GetDevice(ctx, c.db.ORM, deviceID)
	if err != nil {
		return nil, err
	}
	return fromModelDevice(dev), nil
}

func (c *Client) ListDevices(ctx context.Context, serverID string) ([]Device, error) {
	list, err := dbpkg.ListDevices(ctx, c.db.ORM, serverID)
	if err != nil {
		return nil, err
	}
	out := make([]Device, 0, len(list))
	for i := range list {
		out = append(out, *fromModelDevice(&list[i]))
	}
	return out, nil
}

func (c *Client) UpdateDevice(ctx context.Context, d *Device) error {
	return dbpkg.UpdateDevice(ctx, c.db.ORM, toModelDevice(d))
}

func (c *Client) DeleteDevice(ctx context.Context, deviceID string) error {
	return dbpkg.DeleteDevice(ctx, c.db.ORM, deviceID)
}

// SaveDevice is a convenience upsert-like method (delegates to UpdateDevice).
func (c *Client) SaveDevice(ctx context.Context, d *Device) error {
	return dbpkg.UpdateDevice(ctx, c.db.ORM, toModelDevice(d))
}

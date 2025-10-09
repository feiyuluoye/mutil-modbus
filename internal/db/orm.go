package db

import (
    "context"

    "gorm.io/driver/sqlite"
    "gorm.io/gorm"
    "gorm.io/gorm/logger"

    "modbus-simulator/internal/model"
)

// openORM opens a GORM SQLite connection with sane defaults.
func openORM(path string) (*gorm.DB, error) {
    return gorm.Open(sqlite.Open(path), &gorm.Config{
        Logger: logger.Default.LogMode(logger.Warn),
    })
}

// migrateORM ensures the schema for all models exists.
func migrateORM(db *gorm.DB) error {
    return db.AutoMigrate(&model.Server{}, &model.Device{}, &model.PointValue{}, &model.LatestDataValue{})
}

// closeORM closes the underlying SQL DB associated with the GORM connection.
func closeORM(db *gorm.DB) error {
    sqlDB, err := db.DB()
    if err != nil {
        return err
    }
    return sqlDB.Close()
}

// insertPointValue persists a new point value row using the provided context.
func insertPointValue(ctx context.Context, db *gorm.DB, pv *model.PointValue) error {
    return db.WithContext(ctx).Create(pv).Error
}

// InsertPointValuesBatch inserts multiple point values efficiently.
func InsertPointValuesBatch(ctx context.Context, db *gorm.DB, pvs []model.PointValue, batchSize int) error {
    if batchSize <= 0 {
        batchSize = 1000
    }
    if len(pvs) == 0 {
        return nil
    }
    return db.WithContext(ctx).CreateInBatches(pvs, batchSize).Error
}

// upsertServer inserts or updates a server definition.
func upsertServer(ctx context.Context, db *gorm.DB, s *model.Server) error {
    return db.WithContext(ctx).Save(s).Error
}

// CreateServer creates a new server.
func CreateServer(ctx context.Context, db *gorm.DB, s *model.Server) error {
    return db.WithContext(ctx).Create(s).Error
}

// GetServer retrieves a server by server_id.
func GetServer(ctx context.Context, db *gorm.DB, serverID string) (*model.Server, error) {
	var s model.Server
	if err := db.WithContext(ctx).First(&s, "server_id = ?", serverID).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

// ListServers lists all servers ordered by server_id.
func ListServers(ctx context.Context, db *gorm.DB) ([]model.Server, error) {
	var out []model.Server
	if err := db.WithContext(ctx).Order("server_id").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateServer updates an existing server.
func UpdateServer(ctx context.Context, db *gorm.DB, s *model.Server) error {
	return db.WithContext(ctx).Save(s).Error
}

// DeleteServer deletes a server (cascades to devices via FK if configured in DB schema).
func DeleteServer(ctx context.Context, db *gorm.DB, serverID string) error {
	return db.WithContext(ctx).Where("server_id = ?", serverID).Delete(&model.Server{}).Error
}

// upsertDevice inserts or updates a device definition.
func UpsertDevice(ctx context.Context, db *gorm.DB, d *model.Device) error {
	return db.WithContext(ctx).Save(d).Error
}

// CreateDevice creates a new device.
func CreateDevice(ctx context.Context, db *gorm.DB, d *model.Device) error {
	return db.WithContext(ctx).Create(d).Error
}

// GetDevice retrieves a device by device_id.
func GetDevice(ctx context.Context, db *gorm.DB, deviceID string) (*model.Device, error) {
	var dev model.Device
	if err := db.WithContext(ctx).First(&dev, "device_id = ?", deviceID).Error; err != nil {
		return nil, err
	}
	return &dev, nil
}

// ListDevices lists devices; if serverID is non-empty, filters by server.
func ListDevices(ctx context.Context, db *gorm.DB, serverID string) ([]model.Device, error) {
	q := db.WithContext(ctx).Model(&model.Device{})
	if serverID != "" {
		q = q.Where("server_id = ?", serverID)
	}
	var out []model.Device
	if err := q.Order("device_id").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateDevice updates an existing device.
func UpdateDevice(ctx context.Context, db *gorm.DB, d *model.Device) error {
	return db.WithContext(ctx).Save(d).Error
}

// deleteDevice removes a device and cascades associated data.
func deleteDevice(ctx context.Context, db *gorm.DB, deviceID string) error {
	return db.WithContext(ctx).Where("device_id = ?", deviceID).Delete(&model.Device{}).Error
}

// DeleteDevice removes a device by device_id.
func DeleteDevice(ctx context.Context, db *gorm.DB, deviceID string) error {
	return deleteDevice(ctx, db, deviceID)
}

// ListDevicePointValues returns point_values for a device ordered by timestamp desc (and name).
func ListDevicePointValues(ctx context.Context, db *gorm.DB, deviceID string, limit int) ([]model.PointValue, error) {
	q := db.WithContext(ctx).Where("device_id = ?", deviceID).Order("timestamp DESC, name")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var out []model.PointValue
	if err := q.Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// LatestPointsORM returns the latest record per (server_id, device_id, name),
// with optional filters for serverID/deviceID.
func LatestPointsORM(ctx context.Context, db *gorm.DB, serverID, deviceID string) ([]PointLatest, error) {
	// Build subquery grouped by composite key
	sub := db.Table("point_values as p").Joins("JOIN devices d ON d.device_id = p.device_id")
	if serverID != "" {
		sub = sub.Where("d.server_id = ?", serverID)
	}
	if deviceID != "" {
		sub = sub.Where("p.device_id = ?", deviceID)
	}
	sub = sub.Select("d.server_id as server_id, p.device_id as device_id, p.name as name, MAX(p.timestamp) as ts").
		Group("d.server_id, p.device_id, p.name")

	var out []PointLatest
	q := db.WithContext(ctx).Table("point_values as p").
		Joins("JOIN devices d ON d.device_id = p.device_id").
		Joins("JOIN (?) as l ON l.server_id = d.server_id AND l.device_id = p.device_id AND l.name = p.name AND l.ts = p.timestamp", sub).
		Select("d.server_id, p.device_id, p.name, p.address, p.register_type, p.data_type, p.byte_order, p.unit, COALESCE(p.value, 0.0) as value, p.timestamp").
		Order("p.name")
	if err := q.Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

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
    return db.AutoMigrate(&model.Server{}, &model.Device{}, &model.PointValue{})
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

// upsertServer inserts or updates a server definition.
func upsertServer(ctx context.Context, db *gorm.DB, s *model.Server) error {
    return db.WithContext(ctx).Save(s).Error
}

// upsertDevice inserts or updates a device definition.
func upsertDevice(ctx context.Context, db *gorm.DB, d *model.Device) error {
    return db.WithContext(ctx).Save(d).Error
}

// deleteDevice removes a device and cascades associated data.
func deleteDevice(ctx context.Context, db *gorm.DB, deviceID string) error {
    return db.WithContext(ctx).Where("device_id = ?", deviceID).Delete(&model.Device{}).Error
}

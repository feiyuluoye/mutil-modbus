package model

import "time"

// LatestDataValue stores a periodic snapshot of the latest value of each point.
// Table: latest_datas_value
// It mirrors the output from internal/db.PointLatest with an auto-increment ID.
type LatestDataValue struct {
	ID           uint      `gorm:"column:id;primaryKey;autoIncrement"`
	ServerID     string    `gorm:"column:server_id;index"`
	DeviceID     string    `gorm:"column:device_id;index"`
	Name         string    `gorm:"column:name;index"`
	Address      int       `gorm:"column:address"`
	RegisterType string    `gorm:"column:register_type"`
	DataType     string    `gorm:"column:data_type"`
	ByteOrder    string    `gorm:"column:byte_order"`
	Unit         string    `gorm:"column:unit"`
	Value        float64   `gorm:"column:value"`
	Timestamp    time.Time `gorm:"column:timestamp;index"`
}

func (LatestDataValue) TableName() string { return "latest_datas_value" }

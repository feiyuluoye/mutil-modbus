package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// DB wraps sqlite connection
type DB struct {
	SQL *sql.DB
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

package tests

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"modbus-simulator/pkg/modbusdb"
)

func newTestClient(t *testing.T) *modbusdb.Client {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "modbus_test.sqlite")
	client, err := modbusdb.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
	})
	return client
}

func TestServerCRUD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := newTestClient(t)

	srv := &modbusdb.Server{
		ServerID:     "srv-1",
		ServerName:   "Server One",
		Protocol:     "tcp",
		Host:         "127.0.0.1",
		Port:         1502,
		Timeout:      "5s",
		RetryCount:   3,
		Enabled:      true,
		PollInterval: "1s",
	}

	if err := client.CreateServer(ctx, srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	got, err := client.GetServer(ctx, srv.ServerID)
	if err != nil {
		t.Fatalf("GetServer failed: %v", err)
	}
	if got.ServerName != srv.ServerName {
		t.Fatalf("expected server name %q, got %q", srv.ServerName, got.ServerName)
	}

	srv.ServerName = "Server Updated"
	if err := client.UpdateServer(ctx, srv); err != nil {
		t.Fatalf("UpdateServer failed: %v", err)
	}

	if err := client.SaveServer(ctx, srv); err != nil {
		t.Fatalf("SaveServer failed: %v", err)
	}

	list, err := client.ListServers(ctx)
	if err != nil {
		t.Fatalf("ListServers failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 server, got %d", len(list))
	}
	if list[0].ServerName != "Server Updated" {
		t.Fatalf("expected updated server name, got %q", list[0].ServerName)
	}

	if err := client.DeleteServer(ctx, srv.ServerID); err != nil {
		t.Fatalf("DeleteServer failed: %v", err)
	}

	list, err = client.ListServers(ctx)
	if err != nil {
		t.Fatalf("ListServers after delete failed: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 servers after delete, got %d", len(list))
	}
}

func TestDeviceCRUD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := newTestClient(t)

	srv := &modbusdb.Server{ServerID: "srv-device", ServerName: "Device Parent", Protocol: "tcp", Host: "0.0.0.0", Port: 1502}
	if err := client.CreateServer(ctx, srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	dev := &modbusdb.Device{
		DeviceID:     "dev-1",
		ServerID:     srv.ServerID,
		Vendor:       "Acme",
		SlaveID:      5,
		PollInterval: "2s",
	}

	if err := client.CreateDevice(ctx, dev); err != nil {
		t.Fatalf("CreateDevice failed: %v", err)
	}

	got, err := client.GetDevice(ctx, dev.DeviceID)
	if err != nil {
		t.Fatalf("GetDevice failed: %v", err)
	}
	if got.Vendor != dev.Vendor {
		t.Fatalf("expected vendor %q, got %q", dev.Vendor, got.Vendor)
	}

	dev.Vendor = "UpdatedVendor"
	if err := client.UpdateDevice(ctx, dev); err != nil {
		t.Fatalf("UpdateDevice failed: %v", err)
	}

	if err := client.SaveDevice(ctx, dev); err != nil {
		t.Fatalf("SaveDevice failed: %v", err)
	}

	list, err := client.ListDevices(ctx, srv.ServerID)
	if err != nil {
		t.Fatalf("ListDevices failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 device, got %d", len(list))
	}
	if list[0].Vendor != "UpdatedVendor" {
		t.Fatalf("expected updated vendor, got %q", list[0].Vendor)
	}

	if err := client.DeleteDevice(ctx, dev.DeviceID); err != nil {
		t.Fatalf("DeleteDevice failed: %v", err)
	}

	list, err = client.ListDevices(ctx, srv.ServerID)
	if err != nil {
		t.Fatalf("ListDevices after delete failed: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 devices after delete, got %d", len(list))
	}
}

func TestPointOperations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := newTestClient(t)

	srv := &modbusdb.Server{ServerID: "srv-points", ServerName: "Points", Protocol: "tcp", Host: "0.0.0.0", Port: 1502}
	if err := client.CreateServer(ctx, srv); err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	dev := &modbusdb.Device{DeviceID: "dev-points", ServerID: srv.ServerID, Vendor: "Acme", SlaveID: 1}
	if err := client.CreateDevice(ctx, dev); err != nil {
		t.Fatalf("CreateDevice failed: %v", err)
	}

	now := time.Now().UTC()

	single := &modbusdb.PointValue{
		DeviceID:     dev.DeviceID,
		Name:         "temperature",
		Address:      1,
		RegisterType: "holding",
		DataType:     "float32",
		ByteOrder:    "ABCD",
		Scale:        1,
		Unit:         "C",
		Value:        21.5,
		Timestamp:    now,
	}

	if err := client.SavePointValue(ctx, single); err != nil {
		t.Fatalf("SavePointValue failed: %v", err)
	}

	batch := []modbusdb.PointValue{
		{
			DeviceID:     dev.DeviceID,
			Name:         "temperature",
			Address:      1,
			RegisterType: "holding",
			DataType:     "float32",
			ByteOrder:    "ABCD",
			Scale:        1,
			Unit:         "C",
			Value:        22.1,
			Timestamp:    now.Add(1 * time.Minute),
		},
		{
			DeviceID:     dev.DeviceID,
			Name:         "pressure",
			Address:      2,
			RegisterType: "holding",
			DataType:     "float32",
			ByteOrder:    "ABCD",
			Scale:        1,
			Unit:         "bar",
			Value:        1.5,
			Timestamp:    now.Add(2 * time.Minute),
		},
	}

	if err := client.SavePointValuesBatch(ctx, batch, 100); err != nil {
		t.Fatalf("SavePointValuesBatch failed: %v", err)
	}

	latestAll, err := client.LatestPointsAll(ctx)
	if err != nil {
		t.Fatalf("LatestPointsAll failed: %v", err)
	}
	if len(latestAll) != 2 {
		t.Fatalf("expected 2 latest points, got %d", len(latestAll))
	}

	history, err := client.DeviceHistory(ctx, dev.DeviceID, 0)
	if err != nil {
		t.Fatalf("DeviceHistory failed: %v", err)
	}
	if len(history) == 0 {
		t.Fatalf("expected non-empty history")
	}

	limitedHistory, err := client.DeviceHistory(ctx, dev.DeviceID, 2)
	if err != nil {
		t.Fatalf("DeviceHistory with limit failed: %v", err)
	}
	if len(limitedHistory) != 2 {
		t.Fatalf("expected limit=2 to return 2 records, got %d", len(limitedHistory))
	}

	jsonBytes, err := client.StatsJSON(ctx, dev.DeviceID, 2)
	if err != nil {
		t.Fatalf("StatsJSON failed: %v", err)
	}

	var stats map[string]any
	if err := json.Unmarshal(jsonBytes, &stats); err != nil {
		t.Fatalf("StatsJSON produced invalid JSON: %v", err)
	}

	if _, ok := stats["server_count"]; !ok {
		t.Fatalf("expected stats JSON to contain server_count")
	}
	if _, ok := stats["device_points"]; !ok {
		t.Fatalf("expected stats JSON to contain device_points")
	}
}

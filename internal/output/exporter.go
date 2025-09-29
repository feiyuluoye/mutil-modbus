package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"modbus-simulator/internal/model"
)

// WriteJSON writes snapshots to a JSON file with pretty formatting.
func WriteJSON(path string, snaps []model.ServerSnapshot) error {
	b, err := json.MarshalIndent(snaps, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		return fmt.Errorf("write json: %w", err)
	}
	return nil
}

// WriteCSV flattens snapshots and writes to a CSV file.
// Columns: server_id,server_name,address,device_id,point_name,register_type,address_idx,unit,value_uint16,value_bool,timestamp
func WriteCSV(path string, snaps []model.ServerSnapshot) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create csv: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	headers := []string{"server_id", "server_name", "address", "device_id", "point_name", "register_type", "address_idx", "unit", "value_uint16", "value_bool", "timestamp"}
	if err := w.Write(headers); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	for _, s := range snaps {
		for _, d := range s.Devices {
			for _, p := range d.Points {
				var vU16, vBool string
				if p.ValueUint16 != nil {
					vU16 = fmt.Sprintf("%d", *p.ValueUint16)
				}
				if p.ValueBool != nil {
					if *p.ValueBool {
						vBool = "1"
					} else {
						vBool = "0"
					}
				}
				rec := []string{
					s.ServerID,
					s.ServerName,
					s.Address,
					d.DeviceID,
					p.Name,
					p.RegisterType,
					fmt.Sprintf("%d", p.Address),
					p.Unit,
					vU16,
					vBool,
					timeToRFC3339(p.Timestamp),
				}
				if err := w.Write(rec); err != nil {
					return fmt.Errorf("write record: %w", err)
				}
			}
		}
	}
	w.Flush()
	return w.Error()
}

func timeToRFC3339(t time.Time) string { return t.Format(time.RFC3339Nano) }

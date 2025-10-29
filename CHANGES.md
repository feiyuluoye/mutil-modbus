# Multi-Server Simulation Enhancement

## Overview
Modified `cmd/servers/main.go` and `internal/servermgr` to support CSV-based simulation with scale/offset transformations, matching the functionality of the single server implementation (`cmd/server/main.go`).

## Changes Made

### 1. Modified Files

#### `internal/servermgr/manager.go`
- **Added float64 CSV support**: Changed `loadCSV()` to return `[]map[string]float64` instead of `[]map[string]uint16` to support decimal values
- **Added scale/offset transformations**: Modified `applyRowToServer()` to apply scale and offset from point configuration
- **Added multiple data type support**: 
  - `uint16` - unsigned 16-bit integer
  - `int16` - signed 16-bit integer  
  - `float32` - 32-bit floating point (uses 2 consecutive registers)
- **Added helper functions**:
  - `writeNumericRegister()` - writes numeric values with type conversion
  - `setRegisterWord()` - sets single 16-bit register
  - `setRegisterFloat32()` - writes float32 across two registers
  - `floatToUint16()` - converts float64 to uint16 with range checking
  - `floatToInt16()` - converts float64 to int16 with range checking
- **Added CSV file path configuration**: Reads `csv_file` from server config, defaults to `data/topway_dashboard.csv`

#### `internal/collector/config.go`
- **Added CSVFile field** to `ServerConfig` struct for configurable CSV data source

#### `config/topway_config.yaml`
- **Added csv_file configuration** to both servers:
  - `topway_server` (port 1502)
  - `topway_server_two` (port 1503)

### 2. Key Features

#### Scale and Offset Transformation
The system now applies transformations to CSV values before writing to registers:
```
register_value = (csv_value * scale) + offset
```

For example, with `scale: 100` and `offset: 0`:
- CSV value: `2.4` → Register value: `240`
- CSV value: `0.62` → Register value: `62`

#### Data Type Support
- **uint16**: Range 0-65535, rounds to nearest integer
- **int16**: Range -32768 to 32767, rounds to nearest integer
- **float32**: IEEE 754 single precision, stored in 2 consecutive registers (big-endian)

#### CSV Data Format
The CSV file (`data/topway_dashboard.csv`) contains:
- Header row with column names matching point names in config
- Data rows with decimal values
- Example columns: `pretreatment_concentration_mg_l`, `water_treatment_temperature_c`, etc.

### 3. Configuration Example

```yaml
servers:
  - server_id: "topway_server"
    server_name: "Topway Water Treatment"
    protocol: "modbus-tcp"
    connection:
      host: "0.0.0.0"
      port: 1502
    csv_file: "data/topway_dashboard.csv"  # New field
    devices:
      - device_id: "topway_dashboard"
        points:
          - name: "pretreatment_concentration_mg_l"
            address: 0
            register_type: "holding"
            data_type: "uint16"
            scale: 100      # Multiply CSV value by 100
            offset: 0       # Add offset after scaling
            unit: "mg/L"
```

### 4. Behavior

1. **Server startup**: Initializes all registers to zero
2. **CSV loading**: Loads CSV file specified in config (or default)
3. **Initial update**: Applies first CSV row immediately
4. **Periodic updates**: Cycles through CSV rows at configured frequency (default 5s)
5. **Row cycling**: Loops back to first row after reaching the end

### 5. Error Handling

- **CSV not found**: Logs error, server runs but skips periodic updates
- **Invalid CSV format**: Logs error with details
- **Value out of range**: Logs error for specific register
- **Invalid data type**: Logs error and skips register update

## Testing

To test the implementation:

```bash
# Run multi-server simulator
make servers

# Or with explicit config
go run ./cmd/servers --config config/topway_config.yaml

# Take a snapshot after 5 seconds
make servers-snapshot SNAP_WAIT=5s SNAP_JSON=topway.json SNAP_CSV=topway.csv
```

## Compatibility

- Fully backward compatible with existing configurations
- If `csv_file` is not specified, defaults to `data/topway_dashboard.csv`
- If CSV loading fails, server continues to run with zero values
- Existing snapshot and export functionality unchanged

## Benefits

1. **Realistic simulation**: Uses actual decimal values from CSV data
2. **Flexible scaling**: Supports different units and precision requirements
3. **Multiple data types**: Handles various sensor/device data formats
4. **Easy configuration**: CSV file path configurable per server
5. **Consistent behavior**: Matches single server implementation logic

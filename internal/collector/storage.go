package collector

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	dbpkg "modbus-simulator/internal/db"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Storage writes collected PointValue records to JSONL and/or CSV asynchronously.
type Storage struct {
	dir        string
	q          chan PointValue
	wg         sync.WaitGroup
	enableJSON bool
	enableCSV  bool
	enableDB   bool

	jsonFile   *os.File
	jsonWriter *bufio.Writer

	csvFile   *os.File
	csvWriter *csv.Writer

	db     *dbpkg.DB
	closed chan struct{}
}

// NewStorage ensures the output directory exists, opens requested files, and starts background writers.
func NewStorage(dbPath, fileType string, maxWorkers, maxQueue int) (*Storage, error) {
	if dbPath == "" {
		dbPath = "db.sqlite"
	}

	ft := strings.ToLower(strings.TrimSpace(fileType))
	enableJSON := false
	enableCSV := false
	enableDB := false
	switch ft {
	case "json", "jsonl":
		enableJSON = true
	case "csv":
		enableCSV = true
	case "db":
		enableDB = true
	case "json+csv", "csv+json", "both":
		enableJSON = true
		enableCSV = true
	case "json+db", "db+json":
		enableJSON = true
		enableDB = true
	case "csv+db", "db+csv":
		enableCSV = true
		enableDB = true
	case "all", "":
		enableJSON = true
		enableCSV = true
		enableDB = true
	default:
		return nil, fmt.Errorf("unsupported storage file_type %q", fileType)
	}
	if !enableJSON && !enableCSV && !enableDB {
		return nil, errors.New("storage must enable at least one output")
	}

	// Determine output directory for file outputs and the database file path
	var outDir string
	var dbFile string
	base := filepath.Base(dbPath)
	if strings.Contains(base, ".") {
		// dbPath looks like a file path (e.g., ./data.sqlite)
		outDir = filepath.Dir(dbPath)
		dbFile = dbPath
	} else {
		// dbPath is a directory
		outDir = dbPath
		dbFile = filepath.Join(outDir, "data.sqlite")
	}

	s := &Storage{
		dir:        outDir,
		q:          make(chan PointValue, maxQueueIfPositive(maxQueue, 1000)),
		enableJSON: enableJSON,
		enableCSV:  enableCSV,
		enableDB:   enableDB,
		closed:     make(chan struct{}),
	}

	// Ensure outDir exists if we are writing JSON/CSV files
	if (s.enableJSON || s.enableCSV) && strings.TrimSpace(outDir) != "" {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", outDir, err)
		}
	}

	if s.enableJSON {
		jsonPath := filepath.Join(outDir, "collector.jsonl")
		jf, err := os.OpenFile(jsonPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, fmt.Errorf("open json output: %w", err)
		}
		s.jsonFile = jf
		s.jsonWriter = bufio.NewWriterSize(jf, 64*1024)
	}

	if s.enableCSV {
		csvPath := filepath.Join(outDir, "collector.csv")
		cf, err := os.OpenFile(csvPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			if s.jsonFile != nil {
				s.jsonFile.Close()
			}
			return nil, fmt.Errorf("open csv output: %w", err)
		}
		s.csvFile = cf
		s.csvWriter = csv.NewWriter(cf)
		if off, _ := cf.Seek(0, os.SEEK_END); off == 0 {
			header := []string{"timestamp", "server_id", "device_id", "connection", "slave_id", "point_name", "address", "register", "unit", "value"}
			if err := s.csvWriter.Write(header); err != nil {
				if s.jsonFile != nil {
					s.jsonFile.Close()
				}
				cf.Close()
				return nil, fmt.Errorf("write csv header: %w", err)
			}
			s.csvWriter.Flush()
			if err := s.csvWriter.Error(); err != nil {
				if s.jsonFile != nil {
					s.jsonFile.Close()
				}
				cf.Close()
				return nil, err
			}
		}
	}

	if s.enableDB {
		// Ensure parent directory of db file exists
		if dir := filepath.Dir(dbFile); strings.TrimSpace(dir) != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("mkdir %s: %w", dir, err)
			}
		}
		d, err := dbpkg.Open(dbFile)
		if err != nil {
			return nil, fmt.Errorf("open sqlite: %w", err)
		}
		s.db = d
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for v := range s.q {
			if s.enableJSON {
				_ = s.writeJSONL(v)
			}
			if s.enableCSV {
				_ = s.writeCSV(v)
			}
			if s.enableDB {
				_ = s.writeDB(v)
			}
		}
		if s.jsonWriter != nil {
			s.jsonWriter.Flush()
		}
		if s.csvWriter != nil {
			s.csvWriter.Flush()
		}
		close(s.closed)
	}()

	return s, nil
}

func maxQueueIfPositive(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

func (s *Storage) Close() {
	close(s.q)
	<-s.closed
	if s.jsonFile != nil {
		s.jsonFile.Close()
	}
	if s.csvFile != nil {
		s.csvFile.Close()
	}
	if s.db != nil {
		_ = s.db.Close()
	}
}

func (s *Storage) writeJSONL(v PointValue) error {
	if s.jsonWriter == nil {
		return nil
	}
	obj := map[string]any{
		"timestamp":  v.Timestamp.Format(time.RFC3339Nano),
		"server_id":  v.ServerID,
		"device_id":  v.DeviceID,
		"connection": v.Connection,
		"slave_id":   v.SlaveID,
		"point_name": v.PointName,
		"address":    v.Address,
		"register":   v.Register,
		"unit":       v.Unit,
		"raw":        v.Raw,
		"value":      v.Value,
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	if _, err := s.jsonWriter.Write(b); err != nil {
		return err
	}
	if _, err := s.jsonWriter.WriteString("\n"); err != nil {
		return err
	}
	return nil
}

func (s *Storage) writeCSV(v PointValue) error {
	if s.csvWriter == nil {
		return nil
	}
	rec := []string{
		v.Timestamp.Format(time.RFC3339Nano),
		v.ServerID,
		v.DeviceID,
		v.Connection,
		fmt.Sprintf("%d", v.SlaveID),
		v.PointName,
		fmt.Sprintf("%d", v.Address),
		v.Register,
		v.Unit,
		fmt.Sprintf("%g", v.Value),
	}
	if err := s.csvWriter.Write(rec); err != nil {
		return err
	}
	return nil
}

// writeDB persists a PointValue into the sqlite database.
// It maps to the point_values table defined in internal/db/sqlite.go migrate().
// Some columns in the schema (data_type, byte_order) are not available at runtime here;
// we store empty strings for them, and rely on defaults for scale/offset.
func (s *Storage) writeDB(v PointValue) error {
	if s.db == nil || s.db.SQL == nil {
		return nil
	}
	const stmt = `INSERT INTO point_values (
		device_id, name, address, register_type, data_type, byte_order, unit, value, timestamp
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.SQL.Exec(stmt,
		v.DeviceID,
		v.PointName,
		int64(v.Address),
		v.Register,
		"", // data_type not tracked in PointValue
		"", // byte_order not tracked in PointValue
		v.Unit,
		v.Value,
		v.Timestamp,
	)
	return err
}

// Handle implements ResultHandler, enqueueing values for background writers.
func (s *Storage) Handle(v PointValue) error {
	// Best-effort enqueue; avoid blocking indefinitely if queue is full.
	select {
	case s.q <- v:
		return nil
	default:
		// Fallback to blocking to reduce data loss, but with a short timeout.
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case s.q <- v:
			return nil
		case <-timer.C:
			return fmt.Errorf("storage queue full: dropping value %s/%s/%s@%d", v.ServerID, v.DeviceID, v.PointName, v.Address)
		}
	}
}

package collector

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
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

	jsonFile   *os.File
	jsonWriter *bufio.Writer

	csvFile   *os.File
	csvWriter *csv.Writer

	closed chan struct{}
}

// NewStorage ensures the output directory exists, opens requested files, and starts background writers.
func NewStorage(dbPath, fileType string, maxWorkers, maxQueue int) (*Storage, error) {
	if dbPath == "" {
		dbPath = "data"
	}
	if st, err := os.Stat(dbPath); err == nil && !st.IsDir() {
		dbPath = filepath.Dir(dbPath)
	}
	if err := os.MkdirAll(dbPath, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dbPath, err)
	}

	ft := strings.ToLower(strings.TrimSpace(fileType))
	enableJSON := false
	enableCSV := false
	switch ft {
	case "json", "jsonl":
		enableJSON = true
	case "csv":
		enableCSV = true
	case "json+csv", "csv+json", "both", "all", "":
		enableJSON = true
		enableCSV = true
	default:
		return nil, fmt.Errorf("unsupported storage file_type %q", fileType)
	}
	if !enableJSON && !enableCSV {
		return nil, errors.New("storage must enable at least one output")
	}

	s := &Storage{
		dir:        dbPath,
		q:          make(chan PointValue, maxQueueIfPositive(maxQueue, 1000)),
		enableJSON: enableJSON,
		enableCSV:  enableCSV,
		closed:     make(chan struct{}),
	}

	if s.enableJSON {
		jsonPath := filepath.Join(dbPath, "collector.jsonl")
		jf, err := os.OpenFile(jsonPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, fmt.Errorf("open json output: %w", err)
		}
		s.jsonFile = jf
		s.jsonWriter = bufio.NewWriterSize(jf, 64*1024)
	}

	if s.enableCSV {
		csvPath := filepath.Join(dbPath, "collector.csv")
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

func (s *Storage) Handle(v PointValue) error {
	select {
	case s.q <- v:
		return nil
	default:
		return errors.New("storage queue full")
	}
}

// Close stops writers and closes files.
func (s *Storage) Close() {
	close(s.q)
	<-s.closed
	if s.jsonFile != nil {
		s.jsonFile.Close()
	}
	if s.csvFile != nil {
		s.csvFile.Close()
	}
}

func (s *Storage) writeJSONL(v PointValue) error {
	if s.jsonWriter == nil {
		return nil
	}
	obj := map[string]any{
		"timestamp": v.Timestamp.Format(time.RFC3339Nano),
		"server_id": v.ServerID,
		"device_id": v.DeviceID,
		"connection": v.Connection,
		"slave_id": v.SlaveID,
		"point_name": v.PointName,
		"address": v.Address,
		"register": v.Register,
		"unit": v.Unit,
		"raw": v.Raw,
		"value": v.Value,
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

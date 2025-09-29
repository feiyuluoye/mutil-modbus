package collector

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Storage writes collected PointValue to JSONL and CSV asynchronously.
// DBPath is a directory where files will be created/append:
//  - collector.jsonl (one JSON per line)
//  - collector.csv   (header + rows)
// MaxWorkers controls internal buffering, not multiple writers.
// MaxQueueSize controls channel buffer length.
type Storage struct {
	dir          string
	q            chan PointValue
	wg           sync.WaitGroup
	jsonFile     *os.File
	jsonWriter   *bufio.Writer
	csvFile      *os.File
	csvWriter    *csv.Writer
	csvHeaderSet bool
	closed       chan struct{}
}

// NewStorage initializes storage writers. It ensures directory exists and opens files in append mode.
func NewStorage(dbPath string, maxWorkers, maxQueue int) (*Storage, error) {
	if dbPath == "" {
		dbPath = "data"
	}
	// If dbPath points to a file, use its directory.
	st, err := os.Stat(dbPath)
	if err == nil && !st.IsDir() {
		dbPath = filepath.Dir(dbPath)
	}
	if err := os.MkdirAll(dbPath, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dbPath, err)
	}

	jsonPath := filepath.Join(dbPath, "collector.jsonl")
	csvPath := filepath.Join(dbPath, "collector.csv")

	jf, err := os.OpenFile(jsonPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open jsonl: %w", err)
	}
	cf, err := os.OpenFile(csvPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		jf.Close()
		return nil, fmt.Errorf("open csv: %w", err)
	}

	s := &Storage{
		dir:        dbPath,
		q:          make(chan PointValue, maxQueueIfPositive(maxQueue, 1000)),
		jsonFile:   jf,
		jsonWriter: bufio.NewWriterSize(jf, 64*1024),
		csvFile:    cf,
		csvWriter:  csv.NewWriter(cf),
		closed:     make(chan struct{}),
	}

	// initialize CSV header if file is empty
	if off, _ := cf.Seek(0, os.SEEK_END); off == 0 {
		if err := s.csvWriter.Write([]string{"timestamp","server_id","device_id","connection","slave_id","point_name","address","register","unit","value"}); err != nil {
			return nil, fmt.Errorf("write csv header: %w", err)
		}
		s.csvWriter.Flush()
		if err := s.csvWriter.Error(); err != nil {
			return nil, err
		}
		s.csvHeaderSet = true
	} else {
		s.csvHeaderSet = true
	}

	// start single writer goroutine
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for v := range s.q {
			_ = s.writeJSONL(v)
			_ = s.writeCSV(v)
		}
		// flush on exit
		s.jsonWriter.Flush()
		s.csvWriter.Flush()
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

// Handle enqueues a point for persistence.
func (s *Storage) Handle(v PointValue) error {
	select {
	case s.q <- v:
		return nil
	default:
		// best-effort drop with error when queue is full
		return errors.New("storage queue full")
	}
}

// Close stops writers and closes files.
func (s *Storage) Close() {
	close(s.q)
	<-s.closed
	s.jsonFile.Close()
	s.csvFile.Close()
}

func (s *Storage) writeJSONL(v PointValue) error {
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
	// buffered; flush periodically handled by Close()
	return nil
}

func (s *Storage) writeCSV(v PointValue) error {
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
	// buffered; flush periodically handled by Close()
	return nil
}

package main

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Reading is a single sensor data point.
type Reading struct {
	Time   time.Time `json:"t"`
	Sensor string    `json:"s"`
	Value  float64   `json:"v"`
}

// Store persists sensor readings to daily JSONL files.
type Store struct {
	dir string
	mu  sync.Mutex
}

func NewStore(dir string) *Store {
	os.MkdirAll(dir, 0755)
	return &Store{dir: dir}
}

// Record appends a sensor reading to today's file.
func (s *Store) Record(sensor string, value float64, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	filename := filepath.Join(s.dir, t.Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("store: open %s: %v", filename, err)
		return
	}
	defer f.Close()

	r := Reading{Time: t, Sensor: sensor, Value: value}
	json.NewEncoder(f).Encode(r)
}

// Query returns all readings from the given time onward.
func (s *Store) Query(since time.Time) []Reading {
	s.mu.Lock()
	defer s.mu.Unlock()

	var readings []Reading
	start := since.Truncate(24 * time.Hour)
	end := time.Now().Truncate(24 * time.Hour)

	for d := start; !d.After(end); d = d.Add(24 * time.Hour) {
		filename := filepath.Join(s.dir, d.Format("2006-01-02")+".jsonl")
		f, err := os.Open(filename)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			var r Reading
			if json.Unmarshal(scanner.Bytes(), &r) == nil && !r.Time.Before(since) {
				readings = append(readings, r)
			}
		}
		f.Close()
	}

	return readings
}

// Cleanup removes data files older than maxAge.
func (s *Store) Cleanup(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge).Format("2006-01-02")
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Name() < cutoff {
			os.Remove(filepath.Join(s.dir, e.Name()))
		}
	}
}

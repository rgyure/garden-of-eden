package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ReservoirChange is a single full-flush event of the reservoir.
type ReservoirChange struct {
	Date  string `yaml:"date"  json:"date"`
	Notes string `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// ReservoirState is the full reservoir.yml document.
type ReservoirState struct {
	LastChange string            `yaml:"last_change,omitempty" json:"last_change,omitempty"`
	Notes      string            `yaml:"notes,omitempty"       json:"notes,omitempty"`
	History    []ReservoirChange `yaml:"history,omitempty"     json:"history,omitempty"`
}

var reservoirMu sync.Mutex

func (app *App) handleGetReservoir(w http.ResponseWriter, r *http.Request) {
	rs, err := readReservoir(app.config.ReservoirPath)
	if err != nil {
		http.Error(w, "failed to read reservoir state: "+err.Error(), http.StatusInternalServerError)
		return
	}
	response := struct {
		*ReservoirState
		DaysSinceChange int `json:"days_since_change"`
	}{rs, daysSinceLocal(rs.LastChange)}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

// handlePostReservoirChange records that the reservoir was changed today.
// Optional JSON body: {"notes": "added Hydroguard"}
func (app *App) handlePostReservoirChange(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Notes string `json:"notes"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	reservoirMu.Lock()
	defer reservoirMu.Unlock()

	rs, err := readReservoir(app.config.ReservoirPath)
	if err != nil {
		http.Error(w, "failed to read reservoir state: "+err.Error(), http.StatusInternalServerError)
		return
	}

	today := time.Now().Format(dateFormat)
	rs.LastChange = today
	rs.Notes = body.Notes
	rs.History = append(rs.History, ReservoirChange{Date: today, Notes: body.Notes})
	sort.Slice(rs.History, func(i, j int) bool {
		return rs.History[i].Date > rs.History[j].Date
	})

	if err := writeReservoir(app.config.ReservoirPath, rs); err != nil {
		http.Error(w, "failed to write reservoir state: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"last_change":       rs.LastChange,
		"days_since_change": 0,
	})
}

func readReservoir(path string) (*ReservoirState, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &ReservoirState{History: []ReservoirChange{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var rs ReservoirState
	if err := yaml.Unmarshal(data, &rs); err != nil {
		return nil, fmt.Errorf("parse reservoir.yml: %w", err)
	}
	if rs.History == nil {
		rs.History = []ReservoirChange{}
	}
	return &rs, nil
}

func writeReservoir(path string, rs *ReservoirState) error {
	data, err := yaml.Marshal(rs)
	if err != nil {
		return fmt.Errorf("marshal reservoir.yml: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// daysSinceLocal returns integer days from dateStr (YYYY-MM-DD, local time)
// to today. Returns -1 if dateStr is empty or unparseable.
func daysSinceLocal(dateStr string) int {
	if dateStr == "" {
		return -1
	}
	t, err := time.ParseInLocation(dateFormat, dateStr, time.Local)
	if err != nil {
		return -1
	}
	hours := time.Since(t).Hours()
	if hours < 0 {
		return 0
	}
	return int(hours / 24)
}

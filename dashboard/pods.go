package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// PodLayout describes the physical pod grid (e.g. Gardyn 3.0 = 5 columns x 6 rows).
type PodLayout struct {
	Columns int `yaml:"columns" json:"columns"`
	Rows    int `yaml:"rows" json:"rows"`
}

// HarvestEntry records one harvest event from a planting.
type HarvestEntry struct {
	Date    string  `yaml:"date" json:"date"`
	WeightG float64 `yaml:"weight_g,omitempty" json:"weight_g,omitempty"`
	Notes   string  `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// Planting is one plant in one pod over a finite time window.
// When a planting is removed/harvested the same pod can host a new planting
// with a different id, preserving full history.
type Planting struct {
	ID            int            `yaml:"id" json:"id"`
	Pod           string         `yaml:"pod" json:"pod"`
	Variety       string         `yaml:"variety" json:"variety"`
	SeedSource    string         `yaml:"seed_source,omitempty" json:"seed_source,omitempty"`
	Planted       string         `yaml:"planted" json:"planted"`
	Ended         string         `yaml:"ended,omitempty" json:"ended,omitempty"`
	EndReason     string         `yaml:"end_reason,omitempty" json:"end_reason,omitempty"`
	DaysToHarvest int            `yaml:"days_to_harvest,omitempty" json:"days_to_harvest,omitempty"`
	Notes         string         `yaml:"notes,omitempty" json:"notes,omitempty"`
	HarvestLog    []HarvestEntry `yaml:"harvest_log,omitempty" json:"harvest_log,omitempty"`
}

// Inventory is the full pods.yml document.
type Inventory struct {
	Layout    PodLayout  `yaml:"layout" json:"layout"`
	Plantings []Planting `yaml:"plantings" json:"plantings"`
}

const (
	defaultColumns = 3
	defaultRows    = 10
	dateFormat     = "2006-01-02"
)

var (
	podsMu       sync.Mutex
	validReasons = map[string]bool{
		"":          true,
		"harvested": true,
		"failed":    true,
		"removed":   true,
	}
)

func (app *App) handleGetPlantings(w http.ResponseWriter, r *http.Request) {
	inv, err := readInventory(app.config.PodsPath)
	if err != nil {
		http.Error(w, "failed to read plantings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(inv); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

func (app *App) handleUpdatePlantings(w http.ResponseWriter, r *http.Request) {
	var inv Inventory
	if err := json.NewDecoder(r.Body).Decode(&inv); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateInventory(&inv); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	podsMu.Lock()
	defer podsMu.Unlock()

	if err := writeInventory(app.config.PodsPath, &inv); err != nil {
		http.Error(w, "failed to write plantings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func readInventory(path string) (*Inventory, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultInventory(), nil
	}
	if err != nil {
		return nil, err
	}
	var inv Inventory
	if err := yaml.Unmarshal(data, &inv); err != nil {
		return nil, fmt.Errorf("parse pods.yml: %w", err)
	}
	if inv.Layout.Columns <= 0 {
		inv.Layout.Columns = defaultColumns
	}
	if inv.Layout.Rows <= 0 {
		inv.Layout.Rows = defaultRows
	}
	if inv.Plantings == nil {
		inv.Plantings = []Planting{}
	}
	return &inv, nil
}

func writeInventory(path string, inv *Inventory) error {
	data, err := yaml.Marshal(inv)
	if err != nil {
		return fmt.Errorf("marshal pods.yml: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func defaultInventory() *Inventory {
	return &Inventory{
		Layout:    PodLayout{Columns: defaultColumns, Rows: defaultRows},
		Plantings: []Planting{},
	}
}

func validateInventory(inv *Inventory) error {
	if inv.Layout.Columns < 1 || inv.Layout.Columns > 26 {
		return fmt.Errorf("layout.columns must be 1-26")
	}
	if inv.Layout.Rows < 1 || inv.Layout.Rows > 100 {
		return fmt.Errorf("layout.rows must be 1-100")
	}

	seenIDs := make(map[int]bool, len(inv.Plantings))
	activeByPod := make(map[string]int)

	for i, p := range inv.Plantings {
		if p.ID <= 0 {
			return fmt.Errorf("planting[%d]: id must be > 0", i)
		}
		if seenIDs[p.ID] {
			return fmt.Errorf("planting[%d]: duplicate id %d", i, p.ID)
		}
		seenIDs[p.ID] = true

		if p.Pod == "" {
			return fmt.Errorf("planting[%d]: pod is required", i)
		}
		if p.Variety == "" {
			return fmt.Errorf("planting[%d]: variety is required", i)
		}
		if _, err := time.Parse(dateFormat, p.Planted); err != nil {
			return fmt.Errorf("planting[%d]: invalid planted date %q (want YYYY-MM-DD)", i, p.Planted)
		}
		if p.Ended != "" {
			if _, err := time.Parse(dateFormat, p.Ended); err != nil {
				return fmt.Errorf("planting[%d]: invalid ended date %q", i, p.Ended)
			}
		}
		if !validReasons[p.EndReason] {
			return fmt.Errorf("planting[%d]: invalid end_reason %q (want harvested|failed|removed)", i, p.EndReason)
		}
		if p.DaysToHarvest < 0 || p.DaysToHarvest > 365 {
			return fmt.Errorf("planting[%d]: days_to_harvest must be 0-365", i)
		}

		// Only one active planting per pod.
		if p.Ended == "" {
			if other, ok := activeByPod[p.Pod]; ok {
				return fmt.Errorf("pod %s has two active plantings (ids %d and %d)", p.Pod, other, p.ID)
			}
			activeByPod[p.Pod] = p.ID
		}

		for j, h := range p.HarvestLog {
			if _, err := time.Parse(dateFormat, h.Date); err != nil {
				return fmt.Errorf("planting[%d].harvest_log[%d]: invalid date %q", i, j, h.Date)
			}
			if h.WeightG < 0 {
				return fmt.Errorf("planting[%d].harvest_log[%d]: weight_g must be >= 0", i, j)
			}
		}
	}
	return nil
}

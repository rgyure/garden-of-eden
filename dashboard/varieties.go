package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"gopkg.in/yaml.v3"
)

// Variety describes a plant cultivar with target growing parameters.
// Fields beyond Name and DaysToHarvest are reserved for future EC/pH automation.
type Variety struct {
	Name          string  `yaml:"name" json:"name"`
	DaysToHarvest int     `yaml:"days_to_harvest,omitempty" json:"days_to_harvest,omitempty"`
	PHMin         float64 `yaml:"ph_min,omitempty" json:"ph_min,omitempty"`
	PHMax         float64 `yaml:"ph_max,omitempty" json:"ph_max,omitempty"`
	ECMin         float64 `yaml:"ec_min,omitempty" json:"ec_min,omitempty"`
	ECMax         float64 `yaml:"ec_max,omitempty" json:"ec_max,omitempty"`
	LightHours    int     `yaml:"light_hours,omitempty" json:"light_hours,omitempty"`
	Notes         string  `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// VarietyCatalog is the full varieties.yml document.
type VarietyCatalog struct {
	Varieties []Variety `yaml:"varieties" json:"varieties"`
}

func (app *App) handleGetVarieties(w http.ResponseWriter, r *http.Request) {
	cat, err := readVarieties(app.config.VarietiesPath)
	if err != nil {
		http.Error(w, "failed to read varieties: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cat); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

func readVarieties(path string) (*VarietyCatalog, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultVarieties(), nil
	}
	if err != nil {
		return nil, err
	}
	var cat VarietyCatalog
	if err := yaml.Unmarshal(data, &cat); err != nil {
		return nil, fmt.Errorf("parse varieties.yml: %w", err)
	}
	if cat.Varieties == nil {
		cat.Varieties = []Variety{}
	}
	return &cat, nil
}

func defaultVarieties() *VarietyCatalog {
	return &VarietyCatalog{
		Varieties: []Variety{
			{Name: "Genovese Basil", DaysToHarvest: 60, PHMin: 5.5, PHMax: 6.5, ECMin: 1.0, ECMax: 1.6, LightHours: 14},
			{Name: "Thai Basil", DaysToHarvest: 60, PHMin: 5.5, PHMax: 6.5, ECMin: 1.0, ECMax: 1.6, LightHours: 14},
			{Name: "Butterhead Lettuce", DaysToHarvest: 55, PHMin: 5.5, PHMax: 6.5, ECMin: 0.8, ECMax: 1.2, LightHours: 14},
			{Name: "Romaine Lettuce", DaysToHarvest: 65, PHMin: 5.5, PHMax: 6.5, ECMin: 0.8, ECMax: 1.2, LightHours: 14},
			{Name: "Arugula", DaysToHarvest: 40, PHMin: 6.0, PHMax: 7.0, ECMin: 0.8, ECMax: 1.2, LightHours: 14},
			{Name: "Kale", DaysToHarvest: 55, PHMin: 5.5, PHMax: 6.5, ECMin: 1.2, ECMax: 1.8, LightHours: 14},
			{Name: "Swiss Chard", DaysToHarvest: 55, PHMin: 6.0, PHMax: 7.0, ECMin: 1.8, ECMax: 2.3, LightHours: 14},
			{Name: "Cilantro", DaysToHarvest: 50, PHMin: 5.8, PHMax: 6.8, ECMin: 1.2, ECMax: 1.8, LightHours: 14},
			{Name: "Parsley", DaysToHarvest: 75, PHMin: 5.5, PHMax: 6.5, ECMin: 1.8, ECMax: 2.2, LightHours: 14},
			{Name: "Mint", DaysToHarvest: 60, PHMin: 5.5, PHMax: 6.5, ECMin: 1.6, ECMax: 2.0, LightHours: 14},
			{Name: "Spinach", DaysToHarvest: 45, PHMin: 6.0, PHMax: 7.0, ECMin: 1.8, ECMax: 2.3, LightHours: 14},
			{Name: "Bok Choy", DaysToHarvest: 50, PHMin: 6.0, PHMax: 7.0, ECMin: 1.5, ECMax: 2.5, LightHours: 14},
			{Name: "Dill", DaysToHarvest: 55, PHMin: 5.5, PHMax: 6.5, ECMin: 1.0, ECMax: 1.6, LightHours: 14},
			{Name: "Chives", DaysToHarvest: 75, PHMin: 6.0, PHMax: 6.5, ECMin: 1.8, ECMax: 2.2, LightHours: 14},
			{Name: "Cherry Tomato", DaysToHarvest: 80, PHMin: 5.5, PHMax: 6.5, ECMin: 2.0, ECMax: 3.5, LightHours: 16},
			{Name: "Sweet Pepper", DaysToHarvest: 85, PHMin: 5.5, PHMax: 6.5, ECMin: 2.0, ECMax: 3.0, LightHours: 16},
			{Name: "Strawberry", DaysToHarvest: 90, PHMin: 5.5, PHMax: 6.5, ECMin: 1.4, ECMax: 1.8, LightHours: 14},
		},
	}
}

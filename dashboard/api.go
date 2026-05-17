package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Schedule mirrors the structure of schedule.yml.
type Schedule struct {
	Location  LocationConfig  `yaml:"location" json:"location"`
	Light     LightConfig     `yaml:"light" json:"light"`
	Pump      PumpConfig      `yaml:"pump" json:"pump"`
	Scheduler SchedulerConfig `yaml:"scheduler" json:"scheduler"`
}

type LocationConfig struct {
	Name      string  `yaml:"name" json:"name"`
	Latitude  float64 `yaml:"latitude" json:"latitude"`
	Longitude float64 `yaml:"longitude" json:"longitude"`
	Timezone  string  `yaml:"timezone" json:"timezone"`
}

type LightConfig struct {
	DawnBrightness int     `yaml:"dawn_brightness" json:"dawn_brightness"`
	PeakBrightness int     `yaml:"peak_brightness" json:"peak_brightness"`
	CurveFactor    float64 `yaml:"curve_factor" json:"curve_factor"`
}

type PumpConfig struct {
	RunsPerDay         int `yaml:"runs_per_day" json:"runs_per_day"`
	RunDurationMinutes int `yaml:"run_duration_minutes" json:"run_duration_minutes"`
	Speed              int `yaml:"speed" json:"speed"`
}

type SchedulerConfig struct {
	ReconcileIntervalSeconds int `yaml:"reconcile_interval_seconds" json:"reconcile_interval_seconds"`
}

func (app *App) handleGetState(w http.ResponseWriter, r *http.Request) {
	snap := app.state.Snapshot()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(snap)
}

func (app *App) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := app.broker.Subscribe()
	defer app.broker.Unsubscribe(ch)

	// Send current state immediately
	snap := app.state.Snapshot()
	data, _ := json.Marshal(snap)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", event)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (app *App) handleLightCommand(w http.ResponseWriter, r *http.Request) {
	var body struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	state := strings.ToUpper(body.State)
	if state != "ON" && state != "OFF" {
		http.Error(w, "state must be ON or OFF", http.StatusBadRequest)
		return
	}
	app.mqtt.Publish(app.config.BaseTopic+"/light/command", state)
	w.WriteHeader(http.StatusOK)
}

func (app *App) handleLightBrightness(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Value int `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if body.Value < 0 || body.Value > 100 {
		http.Error(w, "value must be 0-100", http.StatusBadRequest)
		return
	}
	app.mqtt.Publish(app.config.BaseTopic+"/light/brightness/set", fmt.Sprintf("%d", body.Value))
	w.WriteHeader(http.StatusOK)
}

func (app *App) handlePumpCommand(w http.ResponseWriter, r *http.Request) {
	var body struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	state := strings.ToUpper(body.State)
	if state != "ON" && state != "OFF" {
		http.Error(w, "state must be ON or OFF", http.StatusBadRequest)
		return
	}
	app.mqtt.Publish(app.config.BaseTopic+"/pump/command", state)
	w.WriteHeader(http.StatusOK)
}

func (app *App) handlePumpSpeed(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Value int `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if body.Value < 0 || body.Value > 100 {
		http.Error(w, "value must be 0-100", http.StatusBadRequest)
		return
	}
	app.mqtt.Publish(app.config.BaseTopic+"/pump/speed/set", fmt.Sprintf("%d", body.Value))
	w.WriteHeader(http.StatusOK)
}

func (app *App) handleOverride(w http.ResponseWriter, r *http.Request) {
	device := r.PathValue("device")
	if device != "light" && device != "pump" {
		http.NotFound(w, r)
		return
	}
	var body struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	state := strings.ToUpper(body.State)
	if state != "ON" && state != "OFF" {
		http.Error(w, "state must be ON or OFF", http.StatusBadRequest)
		return
	}
	app.mqtt.client.Publish(app.config.BaseTopic+"/"+device+"/override", 0, true, state)
	w.WriteHeader(http.StatusOK)
}

func (app *App) handleCameraCapture(w http.ResponseWriter, r *http.Request) {
	app.mqtt.Publish(app.config.BaseTopic+"/image/capture", "")
	w.WriteHeader(http.StatusOK)
}

// handleDoseCommand routes POST /api/dose/{name} body={"volume_ml": float}
// to gardyn/dose/<name>/command. mqtt.py drives the actual peristaltic pump.
func (app *App) handleDoseCommand(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "pump name required", http.StatusBadRequest)
		return
	}
	var body struct {
		VolumeML float64 `json:"volume_ml"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if body.VolumeML <= 0 || body.VolumeML > 100 {
		http.Error(w, "volume_ml must be in (0, 100]", http.StatusBadRequest)
		return
	}
	app.mqtt.Publish(
		fmt.Sprintf("%s/dose/%s/command", app.config.BaseTopic, name),
		fmt.Sprintf("%.2f", body.VolumeML),
	)
	w.WriteHeader(http.StatusAccepted)
}

func (app *App) handleCamera(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var path string
	switch name {
	case "upper":
		path = app.config.UpperImagePath
	case "lower":
		path = app.config.LowerImagePath
	default:
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		http.Error(w, "no image available", http.StatusNotFound)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Photo-Taken", info.ModTime().Format(time.RFC3339))
	http.ServeFile(w, r, path)
}

func (app *App) handleGetSchedule(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(app.config.SchedulePath)
	if err != nil {
		http.Error(w, "failed to read schedule", http.StatusInternalServerError)
		return
	}
	var schedule Schedule
	if err := yaml.Unmarshal(data, &schedule); err != nil {
		http.Error(w, "failed to parse schedule", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(schedule)
}

func (app *App) handleUpdateSchedule(w http.ResponseWriter, r *http.Request) {
	var schedule Schedule
	if err := json.NewDecoder(r.Body).Decode(&schedule); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate
	if schedule.Light.DawnBrightness < 0 || schedule.Light.DawnBrightness > 100 {
		http.Error(w, "dawn_brightness must be 0-100", http.StatusBadRequest)
		return
	}
	if schedule.Light.PeakBrightness < 0 || schedule.Light.PeakBrightness > 100 {
		http.Error(w, "peak_brightness must be 0-100", http.StatusBadRequest)
		return
	}
	if schedule.Light.CurveFactor < 0 || schedule.Light.CurveFactor > 20 {
		http.Error(w, "curve_factor must be 0-20", http.StatusBadRequest)
		return
	}
	if schedule.Pump.RunsPerDay < 0 || schedule.Pump.RunsPerDay > 48 {
		http.Error(w, "runs_per_day must be 0-48", http.StatusBadRequest)
		return
	}
	if schedule.Pump.RunDurationMinutes < 1 || schedule.Pump.RunDurationMinutes > 60 {
		http.Error(w, "run_duration_minutes must be 1-60", http.StatusBadRequest)
		return
	}
	if schedule.Pump.Speed < 0 || schedule.Pump.Speed > 100 {
		http.Error(w, "speed must be 0-100", http.StatusBadRequest)
		return
	}
	if schedule.Scheduler.ReconcileIntervalSeconds < 10 || schedule.Scheduler.ReconcileIntervalSeconds > 3600 {
		http.Error(w, "reconcile_interval_seconds must be 10-3600", http.StatusBadRequest)
		return
	}

	data, err := yaml.Marshal(&schedule)
	if err != nil {
		http.Error(w, "failed to marshal schedule", http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(app.config.SchedulePath, data, 0644); err != nil {
		http.Error(w, "failed to write schedule", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (app *App) handleGetHistory(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	var since time.Time
	switch period {
	case "7d":
		since = time.Now().Add(-7 * 24 * time.Hour)
	case "30d":
		since = time.Now().Add(-30 * 24 * time.Hour)
	default:
		since = time.Now().Add(-24 * time.Hour)
	}
	readings := app.store.Query(since)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"readings": readings})
}

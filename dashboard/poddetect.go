package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// PodChangeEvent records a per-pod difference detected against the baseline image.
// Magnitude is mean per-channel RGB distance (0-255). Higher = more change.
type PodChangeEvent struct {
	Date      string  `json:"date"`
	Camera    string  `json:"camera"`
	Pod       string  `json:"pod"`
	Magnitude float64 `json:"magnitude"`
	Changed   bool    `json:"changed"`
	Status    string  `json:"status"`
}

// ScanResult bundles a per-camera summary returned by handlePodScan.
type ScanResult struct {
	Camera    string           `json:"camera"`
	ScannedAt string           `json:"scanned_at"`
	Threshold float64          `json:"threshold"`
	Events    []PodChangeEvent `json:"events"`
	Error     string           `json:"error,omitempty"`
}

const (
	defaultChangeThreshold = 25.0
	maxEventsToReturn      = 200
)

var eventsMu sync.Mutex

// handlePodScan triggers an on-demand scan of both cameras against their
// baseline images, appends any "changed" events to the events log, and returns
// the full per-pod results.
func (app *App) handlePodScan(w http.ResponseWriter, r *http.Request) {
	cal, err := readCalibration(app.config.PodCalibrationPath)
	if err != nil {
		http.Error(w, "failed to read calibration: "+err.Error(), http.StatusInternalServerError)
		return
	}

	results := app.runScan(cal)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"results": results})
}

// runScan executes scanCamera for upper and lower, persists changed events, and
// returns the combined per-pod results.
func (app *App) runScan(cal *Calibration) []ScanResult {
	results := []ScanResult{}
	for _, side := range []struct {
		name string
		cam  CameraCalibration
	}{
		{"upper", cal.Upper},
		{"lower", cal.Lower},
	} {
		if len(side.cam.Positions) == 0 {
			results = append(results, ScanResult{Camera: side.name, Error: "no calibrated positions"})
			continue
		}
		framePath, _ := app.cameraSourcePath(side.name)
		baselinePath := app.cameraBaselinePath(side.name)
		evts, err := scanCamera(side.name, framePath, baselinePath, side.cam.Positions, defaultChangeThreshold)
		res := ScanResult{
			Camera:    side.name,
			ScannedAt: time.Now().UTC().Format(time.RFC3339),
			Threshold: defaultChangeThreshold,
			Events:    evts,
		}
		if err != nil {
			res.Error = err.Error()
		} else {
			app.persistChangedEvents(evts)
		}
		results = append(results, res)
	}
	return results
}

// scanCamera computes per-pod color difference between the current frame and the
// baseline image, returning one event per calibrated position.
func scanCamera(camera, framePath, baselinePath string, positions []PodPosition, threshold float64) ([]PodChangeEvent, error) {
	frame, err := loadImage(framePath)
	if err != nil {
		return nil, fmt.Errorf("frame: %w", err)
	}
	baseline, err := loadImage(baselinePath)
	if err != nil {
		return nil, fmt.Errorf("baseline: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	events := make([]PodChangeEvent, 0, len(positions))
	for _, p := range positions {
		fr, fg, fb, ok := meanRGB(frame, p.X, p.Y, p.Radius)
		if !ok {
			continue
		}
		br, bg, bb, ok := meanRGB(baseline, p.X, p.Y, p.Radius)
		if !ok {
			continue
		}
		dr := fr - br
		dg := fg - bg
		db := fb - bb
		mag := math.Sqrt(dr*dr+dg*dg+db*db) / math.Sqrt(3)
		events = append(events, PodChangeEvent{
			Date:      now,
			Camera:    camera,
			Pod:       p.Pod,
			Magnitude: math.Round(mag*100) / 100,
			Changed:   mag >= threshold,
			Status:    "pending",
		})
	}
	return events, nil
}

func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", filepath.Base(path), err)
	}
	return img, nil
}

// meanRGB returns the average per-channel value (0-255) inside a circle at
// (cx, cy) with radius r, clipped to image bounds. ok=false if the circle has
// no in-bounds pixels.
func meanRGB(img image.Image, cx, cy, r int) (float64, float64, float64, bool) {
	bounds := img.Bounds()
	var rSum, gSum, bSum uint64
	var count uint64
	r2 := r * r
	minX, maxX := cx-r, cx+r
	minY, maxY := cy-r, cy+r
	if minX < bounds.Min.X {
		minX = bounds.Min.X
	}
	if maxX >= bounds.Max.X {
		maxX = bounds.Max.X - 1
	}
	if minY < bounds.Min.Y {
		minY = bounds.Min.Y
	}
	if maxY >= bounds.Max.Y {
		maxY = bounds.Max.Y - 1
	}
	for y := minY; y <= maxY; y++ {
		dy := y - cy
		dy2 := dy * dy
		for x := minX; x <= maxX; x++ {
			dx := x - cx
			if dx*dx+dy2 > r2 {
				continue
			}
			pr, pg, pb, _ := img.At(x, y).RGBA()
			rSum += uint64(pr >> 8)
			gSum += uint64(pg >> 8)
			bSum += uint64(pb >> 8)
			count++
		}
	}
	if count == 0 {
		return 0, 0, 0, false
	}
	return float64(rSum) / float64(count),
		float64(gSum) / float64(count),
		float64(bSum) / float64(count),
		true
}

// persistChangedEvents appends only Changed=true events to the events log.
func (app *App) persistChangedEvents(events []PodChangeEvent) {
	changed := make([]PodChangeEvent, 0, len(events))
	for _, e := range events {
		if e.Changed {
			changed = append(changed, e)
		}
	}
	if len(changed) == 0 {
		return
	}
	eventsMu.Lock()
	defer eventsMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(app.config.PodEventsPath), 0755); err != nil {
		return
	}
	f, err := os.OpenFile(app.config.PodEventsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range changed {
		_ = enc.Encode(e)
	}
}

// handleGetPodEvents returns recent events from the JSONL log (newest first).
func (app *App) handleGetPodEvents(w http.ResponseWriter, r *http.Request) {
	events, err := readPodEvents(app.config.PodEventsPath, maxEventsToReturn)
	if err != nil {
		http.Error(w, "failed to read events: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"events": events})
}

func readPodEvents(path string, limit int) ([]PodChangeEvent, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return []PodChangeEvent{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	events := []PodChangeEvent{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e PodChangeEvent
		if err := json.Unmarshal(line, &e); err == nil {
			events = append(events, e)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	if limit > 0 && len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

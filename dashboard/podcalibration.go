package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// PodPosition is the pixel-space center and radius of a pod in a camera frame.
type PodPosition struct {
	Pod    string `yaml:"pod" json:"pod"`
	X      int    `yaml:"x" json:"x"`
	Y      int    `yaml:"y" json:"y"`
	Radius int    `yaml:"radius" json:"radius"`
}

// CameraCalibration captures pod positions for one camera plus the frame size
// they were calibrated against.
type CameraCalibration struct {
	Width      int           `yaml:"width,omitempty" json:"width,omitempty"`
	Height     int           `yaml:"height,omitempty" json:"height,omitempty"`
	BaselineAt string        `yaml:"baseline_at,omitempty" json:"baseline_at,omitempty"`
	Positions  []PodPosition `yaml:"positions" json:"positions"`
}

// Calibration is the full pod_calibration.yml document.
type Calibration struct {
	Upper CameraCalibration `yaml:"upper" json:"upper"`
	Lower CameraCalibration `yaml:"lower" json:"lower"`
}

var calibrationMu sync.Mutex

func (app *App) handleGetPodCalibration(w http.ResponseWriter, r *http.Request) {
	cal, err := readCalibration(app.config.PodCalibrationPath)
	if err != nil {
		http.Error(w, "failed to read calibration: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cal); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

func (app *App) handleUpdatePodCalibration(w http.ResponseWriter, r *http.Request) {
	var cal Calibration
	if err := json.NewDecoder(r.Body).Decode(&cal); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateCalibration(&cal); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	calibrationMu.Lock()
	defer calibrationMu.Unlock()
	if err := writeCalibration(app.config.PodCalibrationPath, &cal); err != nil {
		http.Error(w, "failed to write calibration: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleCaptureBaseline copies the current camera snapshot to the persistent
// baseline path that pod scans compare against.
func (app *App) handleCaptureBaseline(w http.ResponseWriter, r *http.Request) {
	cam := r.PathValue("camera")
	srcPath, err := app.cameraSourcePath(cam)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dstPath := app.cameraBaselinePath(cam)
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		http.Error(w, "failed to ensure baseline dir: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := copyFile(srcPath, dstPath); err != nil {
		http.Error(w, "failed to capture baseline: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"camera":   cam,
		"baseline": dstPath,
	})
}

// cameraSourcePath maps a camera name to its live snapshot path.
func (app *App) cameraSourcePath(cam string) (string, error) {
	switch cam {
	case "upper":
		return app.config.UpperImagePath, nil
	case "lower":
		return app.config.LowerImagePath, nil
	default:
		return "", fmt.Errorf("unknown camera %q", cam)
	}
}

// cameraBaselinePath maps a camera name to its persistent baseline image.
func (app *App) cameraBaselinePath(cam string) string {
	return filepath.Join(app.config.PodBaselineDir, cam+".jpg")
}

func readCalibration(path string) (*Calibration, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Calibration{
			Upper: CameraCalibration{Positions: []PodPosition{}},
			Lower: CameraCalibration{Positions: []PodPosition{}},
		}, nil
	}
	if err != nil {
		return nil, err
	}
	var cal Calibration
	if err := yaml.Unmarshal(data, &cal); err != nil {
		return nil, fmt.Errorf("parse pod_calibration.yml: %w", err)
	}
	if cal.Upper.Positions == nil {
		cal.Upper.Positions = []PodPosition{}
	}
	if cal.Lower.Positions == nil {
		cal.Lower.Positions = []PodPosition{}
	}
	return &cal, nil
}

func writeCalibration(path string, cal *Calibration) error {
	data, err := yaml.Marshal(cal)
	if err != nil {
		return fmt.Errorf("marshal pod_calibration.yml: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func validateCalibration(cal *Calibration) error {
	for _, side := range []struct {
		name string
		cam  CameraCalibration
	}{
		{"upper", cal.Upper},
		{"lower", cal.Lower},
	} {
		seen := make(map[string]bool, len(side.cam.Positions))
		for i, p := range side.cam.Positions {
			if p.Pod == "" {
				return fmt.Errorf("%s.positions[%d]: pod is required", side.name, i)
			}
			if seen[p.Pod] {
				return fmt.Errorf("%s.positions[%d]: duplicate pod %s", side.name, i, p.Pod)
			}
			seen[p.Pod] = true
			if p.X < 0 || p.Y < 0 {
				return fmt.Errorf("%s.positions[%d] (%s): x/y must be >= 0", side.name, i, p.Pod)
			}
			if p.Radius < 3 || p.Radius > 200 {
				return fmt.Errorf("%s.positions[%d] (%s): radius must be 3-200", side.name, i, p.Pod)
			}
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

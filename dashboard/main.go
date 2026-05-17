package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

//go:embed static
var staticFS embed.FS

type Config struct {
	MQTTBroker     string
	MQTTPort       int
	MQTTUsername   string
	MQTTPassword   string
	BaseTopic      string
	Identifier     string
	HTTPPort       int
	SchedulePath       string
	PodsPath           string
	VarietiesPath      string
	PodCalibrationPath string
	PodBaselineDir     string
	PodEventsPath      string
	DataDir            string
	UpperImagePath     string
	LowerImagePath     string
}

type App struct {
	config Config
	state  *State
	store  *Store
	broker *SSEBroker
	mqtt   *MQTTConn
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func loadConfig() Config {
	godotenv.Load()
	port, _ := strconv.Atoi(getEnv("MQTT_PORT", "1883"))
	httpPort, _ := strconv.Atoi(getEnv("DASHBOARD_PORT", "8080"))

	return Config{
		MQTTBroker:     getEnv("MQTT_BROKER", "localhost"),
		MQTTPort:       port,
		MQTTUsername:   getEnv("MQTT_USERNAME", ""),
		MQTTPassword:   getEnv("MQTT_PASSWORD", ""),
		BaseTopic:      getEnv("MQTT_BASETOPIC", "gardyn"),
		Identifier:     getEnv("MQTT_IDENTIFIER", "gardyn"),
		HTTPPort:       httpPort,
		SchedulePath:   getEnv("SCHEDULE_PATH", "schedule.yml"),
		PodsPath:           getEnv("PODS_PATH", "pods.yml"),
		VarietiesPath:      getEnv("VARIETIES_PATH", "varieties.yml"),
		PodCalibrationPath: getEnv("POD_CALIBRATION_PATH", "pod_calibration.yml"),
		PodBaselineDir:     getEnv("POD_BASELINE_DIR", "pod_baselines"),
		PodEventsPath:      getEnv("POD_EVENTS_PATH", "pod_events.jsonl"),
		DataDir:            getEnv("DASHBOARD_DATA_DIR", "data"),
		UpperImagePath:     getEnv("UPPER_IMAGE_PATH", "/tmp/upper_camera.jpg"),
		LowerImagePath:     getEnv("LOWER_IMAGE_PATH", "/tmp/lower_camera.jpg"),
	}
}

func main() {
	cfg := loadConfig()

	store := NewStore(cfg.DataDir)
	store.Cleanup(30 * 24 * time.Hour)

	app := &App{
		config: cfg,
		state:  NewState(),
		store:  store,
		broker: NewSSEBroker(),
	}

	app.mqtt = NewMQTTConn(app)

	scanHour, _ := strconv.Atoi(getEnv("POD_SCAN_HOUR", "12"))
	if scanHour < 0 || scanHour > 23 {
		scanHour = 12
	}
	app.startDailyScan(scanHour)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/state", app.handleGetState)
	mux.HandleFunc("GET /api/events", app.handleSSE)
	mux.HandleFunc("POST /api/light/command", app.handleLightCommand)
	mux.HandleFunc("POST /api/light/brightness", app.handleLightBrightness)
	mux.HandleFunc("POST /api/pump/command", app.handlePumpCommand)
	mux.HandleFunc("POST /api/pump/speed", app.handlePumpSpeed)
	mux.HandleFunc("POST /api/override/{device}", app.handleOverride)
	mux.HandleFunc("POST /api/camera/capture", app.handleCameraCapture)
	mux.HandleFunc("GET /api/camera/{name}", app.handleCamera)
	mux.HandleFunc("GET /api/schedule", app.handleGetSchedule)
	mux.HandleFunc("PUT /api/schedule", app.handleUpdateSchedule)
	mux.HandleFunc("GET /api/plantings", app.handleGetPlantings)
	mux.HandleFunc("PUT /api/plantings", app.handleUpdatePlantings)
	mux.HandleFunc("GET /api/varieties", app.handleGetVarieties)
	mux.HandleFunc("GET /api/pod-calibration", app.handleGetPodCalibration)
	mux.HandleFunc("PUT /api/pod-calibration", app.handleUpdatePodCalibration)
	mux.HandleFunc("POST /api/pod-baseline/{camera}", app.handleCaptureBaseline)
	mux.HandleFunc("POST /api/pod-scan", app.handlePodScan)
	mux.HandleFunc("GET /api/pod-events", app.handleGetPodEvents)
	mux.HandleFunc("POST /api/dose/{name}", app.handleDoseCommand)
	mux.HandleFunc("GET /api/history", app.handleGetHistory)

	staticContent, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /", http.FileServer(http.FS(staticContent)))

	addr := fmt.Sprintf(":%d", cfg.HTTPPort)
	log.Printf("Garden of Eden dashboard on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// State holds the current known state of all sensors and controls.
type State struct {
	mu sync.RWMutex

	Connected       bool     `json:"connected"`
	LightState      string   `json:"light_state"`
	LightBrightness int      `json:"light_brightness"`
	PumpState       string   `json:"pump_state"`
	PumpSpeed       int      `json:"pump_speed"`
	Temperature     *float64 `json:"temperature"`
	Humidity        *float64 `json:"humidity"`
	PCBTemp         *float64 `json:"pcb_temp"`
	WaterLevel      *float64 `json:"water_level"`
	WaterLowState   string   `json:"water_low_state"`
	WaterLowCM      *float64 `json:"water_low_cm"`
	WaterLowMode    string   `json:"water_low_mode"`
	LightOverride   bool     `json:"light_override"`
	PumpOverride    bool     `json:"pump_override"`
}

func NewState() *State {
	return &State{
		LightState: "OFF",
		PumpState:  "OFF",
	}
}

func (s *State) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := *s
	return cp
}

// SSEBroker fans out events to connected SSE clients.
type SSEBroker struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

func NewSSEBroker() *SSEBroker {
	return &SSEBroker{clients: make(map[chan string]struct{})}
}

func (b *SSEBroker) Subscribe() chan string {
	ch := make(chan string, 16)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *SSEBroker) Unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
	close(ch)
}

func (b *SSEBroker) Publish(event string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- event:
		default: // slow client, drop
		}
	}
}

// MQTTConn wraps the paho MQTT client.
type MQTTConn struct {
	client mqtt.Client
	app    *App
}

func NewMQTTConn(app *App) *MQTTConn {
	cfg := app.config
	clientID := fmt.Sprintf("%s_dashboard", cfg.Identifier)

	opts := mqtt.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%d", cfg.MQTTBroker, cfg.MQTTPort)).
		SetClientID(clientID).
		SetUsername(cfg.MQTTUsername).
		SetPassword(cfg.MQTTPassword).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second)

	conn := &MQTTConn{app: app}

	opts.SetOnConnectHandler(func(c mqtt.Client) {
		log.Printf("MQTT connected")
		app.state.mu.Lock()
		app.state.Connected = true
		app.state.mu.Unlock()
		conn.broadcastState()

		topic := cfg.BaseTopic + "/#"
		c.Subscribe(topic, 0, conn.onMessage)
	})

	opts.SetConnectionLostHandler(func(c mqtt.Client, err error) {
		log.Printf("MQTT disconnected: %v", err)
		app.state.mu.Lock()
		app.state.Connected = false
		app.state.mu.Unlock()
		conn.broadcastState()
	})

	conn.client = mqtt.NewClient(opts)
	if token := conn.client.Connect(); token.Wait() && token.Error() != nil {
		log.Printf("MQTT initial connect failed (will retry): %v", token.Error())
	}

	return conn
}

func (m *MQTTConn) Publish(topic string, payload string) {
	m.client.Publish(topic, 0, false, payload)
}

func (m *MQTTConn) onMessage(_ mqtt.Client, msg mqtt.Message) {
	topic := msg.Topic()
	prefix := m.app.config.BaseTopic + "/"

	if !strings.HasPrefix(topic, prefix) {
		return
	}
	suffix := strings.TrimPrefix(topic, prefix)

	// Skip binary topics
	if strings.HasPrefix(suffix, "image/") {
		return
	}

	payload := string(msg.Payload())
	now := time.Now()
	changed := false

	m.app.state.mu.Lock()
	switch suffix {
	case "light/state":
		m.app.state.LightState = strings.ToUpper(payload)
		changed = true
	case "light/brightness/state":
		if v, err := strconv.Atoi(payload); err == nil {
			m.app.state.LightBrightness = v
			changed = true
		}
	case "pump/state":
		m.app.state.PumpState = strings.ToUpper(payload)
		changed = true
	case "pump/speed/state":
		if v, err := strconv.Atoi(payload); err == nil {
			m.app.state.PumpSpeed = v
			changed = true
		}
	case "temperature":
		if v, err := strconv.ParseFloat(payload, 64); err == nil {
			m.app.state.Temperature = &v
			m.app.store.Record("temperature", v, now)
			changed = true
		}
	case "humidity":
		if v, err := strconv.ParseFloat(payload, 64); err == nil {
			m.app.state.Humidity = &v
			m.app.store.Record("humidity", v, now)
			changed = true
		}
	case "pcb/temperature":
		if v, err := strconv.ParseFloat(payload, 64); err == nil {
			m.app.state.PCBTemp = &v
			m.app.store.Record("pcb_temp", v, now)
			changed = true
		}
	case "water/level":
		if v, err := strconv.ParseFloat(payload, 64); err == nil {
			m.app.state.WaterLevel = &v
			m.app.store.Record("water_level", v, now)
			changed = true
		}
	case "water/low/state":
		m.app.state.WaterLowState = strings.ToUpper(payload)
		changed = true
	case "water/low/cm":
		if v, err := strconv.ParseFloat(payload, 64); err == nil {
			m.app.state.WaterLowCM = &v
			changed = true
		}
	case "water/low/mode":
		m.app.state.WaterLowMode = payload
		changed = true
	case "light/override":
		m.app.state.LightOverride = strings.ToUpper(payload) == "ON"
		changed = true
	case "pump/override":
		m.app.state.PumpOverride = strings.ToUpper(payload) == "ON"
		changed = true
	}
	m.app.state.mu.Unlock()

	if changed {
		m.broadcastState()
	}
}

func (m *MQTTConn) broadcastState() {
	snap := m.app.state.Snapshot()
	data, _ := json.Marshal(snap)
	m.app.broker.Publish(string(data))
}

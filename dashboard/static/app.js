let state = {};
let schedule = {};
let chart = null;
let editing = false;

// --- Init ---
document.addEventListener('DOMContentLoaded', () => {
  connectSSE();
  loadSchedule();
  loadHistory('24h');
});

// --- SSE ---
function connectSSE() {
  const es = new EventSource('/api/events');

  es.onmessage = (e) => {
    state = JSON.parse(e.data);
    updateUI();
  };

  es.onerror = () => {
    setConnectionStatus(false);
  };
}

// --- UI Updates ---
function updateUI() {
  setConnectionStatus(state.connected);

  // Light
  const lightToggle = document.getElementById('light-toggle');
  const lightLabel = document.getElementById('light-state');
  lightToggle.checked = state.light_state === 'ON';
  lightLabel.textContent = state.light_state || 'OFF';
  lightLabel.className = 'state-label ' + (state.light_state === 'ON' ? 'on' : 'off');

  const brightnessSlider = document.getElementById('brightness-slider');
  if (document.activeElement !== brightnessSlider) {
    brightnessSlider.value = state.light_brightness || 0;
    document.getElementById('brightness-val').textContent = (state.light_brightness || 0) + '%';
  }

  // Pump
  const pumpToggle = document.getElementById('pump-toggle');
  const pumpLabel = document.getElementById('pump-state');
  pumpToggle.checked = state.pump_state === 'ON';
  pumpLabel.textContent = state.pump_state || 'OFF';
  pumpLabel.className = 'state-label ' + (state.pump_state === 'ON' ? 'on' : 'off');

  const speedSlider = document.getElementById('speed-slider');
  if (document.activeElement !== speedSlider) {
    speedSlider.value = state.pump_speed || 0;
    document.getElementById('speed-val').textContent = (state.pump_speed || 0) + '%';
  }

  // Sensors
  setVal('temperature', state.temperature);
  setVal('humidity', state.humidity);
  setVal('pcb-temp', state.pcb_temp);
  setVal('water-level', state.water_level);

  // Overrides
  const lightOverride = document.getElementById('light-override');
  lightOverride.checked = state.light_override;
  document.getElementById('light-card').classList.toggle('overriding', state.light_override);

  const pumpOverride = document.getElementById('pump-override');
  pumpOverride.checked = state.pump_override;
  document.getElementById('pump-card').classList.toggle('overriding', state.pump_override);

  // Water low
  const waterLow = document.getElementById('water-low');
  const waterCard = document.getElementById('water-low-card');
  if (state.water_low_state === 'ON') {
    waterLow.textContent = 'LOW';
    waterCard.classList.add('warning');
  } else {
    waterLow.textContent = 'OK';
    waterCard.classList.remove('warning');
  }
  document.getElementById('water-low-mode').textContent = state.water_low_mode || '';
}

function setVal(id, value) {
  const el = document.getElementById(id);
  if (value !== null && value !== undefined) {
    el.textContent = Number(value).toFixed(1);
  } else {
    el.textContent = '--';
  }
}

function setConnectionStatus(connected) {
  const el = document.getElementById('conn-status');
  if (connected) {
    el.textContent = 'Connected';
    el.className = 'status connected';
  } else {
    el.textContent = 'Disconnected';
    el.className = 'status disconnected';
  }
}

// --- Commands ---
async function postJSON(url, body) {
  try {
    await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    });
  } catch (err) {
    console.error('Command failed:', err);
  }
}

function toggleLight() {
  const newState = document.getElementById('light-toggle').checked ? 'ON' : 'OFF';
  postJSON('/api/light/command', { state: newState });
}

function setBrightness(value) {
  postJSON('/api/light/brightness', { value: parseInt(value) });
}

function togglePump() {
  const newState = document.getElementById('pump-toggle').checked ? 'ON' : 'OFF';
  postJSON('/api/pump/command', { state: newState });
}

function setPumpSpeed(value) {
  postJSON('/api/pump/speed', { value: parseInt(value) });
}

function toggleOverride(device) {
  const checked = document.getElementById(device + '-override').checked;
  postJSON('/api/override/' + device, { state: checked ? 'ON' : 'OFF' });
}

// --- Cameras ---
function refreshCameras() {
  const ts = Date.now();
  document.getElementById('camera-upper').src = '/api/camera/upper?t=' + ts;
  document.getElementById('camera-lower').src = '/api/camera/lower?t=' + ts;

  // Reset error state
  document.getElementById('camera-upper').classList.remove('no-image');
  document.getElementById('camera-lower').classList.remove('no-image');
}

// --- Schedule ---
async function loadSchedule() {
  try {
    const res = await fetch('/api/schedule');
    schedule = await res.json();
    renderSchedule();
  } catch (err) {
    console.error('Failed to load schedule:', err);
  }
}

function renderSchedule() {
  document.getElementById('sched-location').textContent = schedule.location?.name || '--';
  document.getElementById('sched-dawn').textContent = (schedule.light?.dawn_brightness ?? '--') + '%';
  document.getElementById('sched-peak').textContent = (schedule.light?.peak_brightness ?? '--') + '%';
  document.getElementById('sched-curve').textContent = schedule.light?.curve_factor ?? '--';
  document.getElementById('sched-runs').textContent = schedule.pump?.runs_per_day ?? '--';
  document.getElementById('sched-duration').textContent = (schedule.pump?.run_duration_minutes ?? '--') + ' min';
  document.getElementById('sched-speed').textContent = (schedule.pump?.speed ?? '--') + '%';
  document.getElementById('sched-interval').textContent = (schedule.scheduler?.reconcile_interval_seconds ?? '--') + 's';
}

function toggleScheduleEdit() {
  editing = !editing;
  const display = document.getElementById('schedule-display');
  const form = document.getElementById('schedule-form');
  const btn = document.getElementById('schedule-edit-btn');

  if (editing) {
    display.classList.add('hidden');
    form.classList.remove('hidden');
    btn.textContent = 'Cancel';

    // Populate form
    document.getElementById('edit-dawn').value = schedule.light?.dawn_brightness ?? 40;
    document.getElementById('edit-peak').value = schedule.light?.peak_brightness ?? 100;
    document.getElementById('edit-curve').value = schedule.light?.curve_factor ?? 5;
    document.getElementById('edit-runs').value = schedule.pump?.runs_per_day ?? 2;
    document.getElementById('edit-duration').value = schedule.pump?.run_duration_minutes ?? 2;
    document.getElementById('edit-speed').value = schedule.pump?.speed ?? 60;
    document.getElementById('edit-interval').value = schedule.scheduler?.reconcile_interval_seconds ?? 60;
  } else {
    display.classList.remove('hidden');
    form.classList.add('hidden');
    btn.textContent = 'Edit';
  }
}

async function saveSchedule(e) {
  e.preventDefault();

  const updated = {
    location: schedule.location,
    light: {
      dawn_brightness: parseInt(document.getElementById('edit-dawn').value),
      peak_brightness: parseInt(document.getElementById('edit-peak').value),
      curve_factor: parseFloat(document.getElementById('edit-curve').value)
    },
    pump: {
      runs_per_day: parseInt(document.getElementById('edit-runs').value),
      run_duration_minutes: parseInt(document.getElementById('edit-duration').value),
      speed: parseInt(document.getElementById('edit-speed').value)
    },
    scheduler: {
      reconcile_interval_seconds: parseInt(document.getElementById('edit-interval').value)
    }
  };

  try {
    const res = await fetch('/api/schedule', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(updated)
    });

    if (res.ok) {
      schedule = updated;
      renderSchedule();
      toggleScheduleEdit();
    } else {
      const msg = await res.text();
      alert('Failed to save: ' + msg);
    }
  } catch (err) {
    alert('Failed to save schedule');
  }
}

// --- History Chart ---
async function loadHistory(period, btn) {
  // Update active button
  if (btn) {
    document.querySelectorAll('.period-selector .btn').forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
  }

  try {
    const res = await fetch('/api/history?period=' + period);
    const data = await res.json();
    renderChart(data.readings || []);
  } catch (err) {
    console.error('Failed to load history:', err);
  }
}

function renderChart(readings) {
  const canvas = document.getElementById('history-chart');
  const emptyMsg = document.getElementById('chart-empty');

  if (!readings.length) {
    canvas.classList.add('hidden');
    emptyMsg.classList.remove('hidden');
    return;
  }
  canvas.classList.remove('hidden');
  emptyMsg.classList.add('hidden');

  // Group by sensor
  const groups = {};
  readings.forEach(r => {
    if (!groups[r.s]) groups[r.s] = [];
    groups[r.s].push({ x: new Date(r.t), y: r.v });
  });

  const datasets = [];
  const sensorConfig = {
    temperature:  { label: 'Temperature',  color: '#4caf50', yAxisID: 'y' },
    pcb_temp:     { label: 'PCB Temp',     color: '#ff9800', yAxisID: 'y' },
    humidity:     { label: 'Humidity',      color: '#2196f3', yAxisID: 'y1' },
    water_level:  { label: 'Water Level',  color: '#00bcd4', yAxisID: 'y' }
  };

  for (const [sensor, cfg] of Object.entries(sensorConfig)) {
    if (groups[sensor]) {
      datasets.push({
        label: cfg.label,
        data: groups[sensor],
        borderColor: cfg.color,
        backgroundColor: cfg.color + '20',
        borderWidth: 2,
        pointRadius: 2,
        tension: 0.3,
        yAxisID: cfg.yAxisID
      });
    }
  }

  if (chart) {
    chart.destroy();
  }

  const ctx = canvas.getContext('2d');
  chart = new Chart(ctx, {
    type: 'line',
    data: { datasets },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: 'index', intersect: false },
      scales: {
        x: {
          type: 'time',
          time: { tooltipFormat: 'MMM d, h:mm a' },
          ticks: { maxTicksLimit: 8, font: { size: 11 } },
          grid: { color: '#f0f0f0' }
        },
        y: {
          position: 'left',
          title: { display: true, text: 'Temperature / Level' },
          ticks: { font: { size: 11 } },
          grid: { color: '#f0f0f0' }
        },
        y1: {
          position: 'right',
          title: { display: true, text: 'Humidity %' },
          ticks: { font: { size: 11 } },
          grid: { drawOnChartArea: false },
          min: 0,
          max: 100
        }
      },
      plugins: {
        legend: { position: 'bottom', labels: { boxWidth: 12, font: { size: 12 } } }
      }
    }
  });
}

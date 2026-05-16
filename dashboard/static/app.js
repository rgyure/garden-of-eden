let state = {};
let schedule = {};
let inventory = { layout: { columns: 3, rows: 10 }, plantings: [] };
let activePodId = null;
let chart = null;
let editing = false;

const STAGE_PRESETS = {
  germination: { runs_per_day: 2, run_duration_minutes: 1 },
  seedling:    { runs_per_day: 4, run_duration_minutes: 2 },
  vegetative:  { runs_per_day: 6, run_duration_minutes: 2 },
  flowering:   { runs_per_day: 8, run_duration_minutes: 3 }
};

function inferStage(pump) {
  if (!pump) return 'custom';
  for (const [key, p] of Object.entries(STAGE_PRESETS)) {
    if (pump.runs_per_day === p.runs_per_day && pump.run_duration_minutes === p.run_duration_minutes) {
      return key;
    }
  }
  return 'custom';
}

// --- Init ---
document.addEventListener('DOMContentLoaded', () => {
  connectSSE();
  loadSchedule();
  loadInventory();
  loadHistory('24h');
  refreshCameras();
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
  setVal('temperature', cToF(state.temperature));
  setVal('humidity', state.humidity);
  setVal('pcb-temp', cToF(state.pcb_temp));
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

function cToF(c) {
  if (c === null || c === undefined) return c;
  return c * 9 / 5 + 32;
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
async function capturePhotos() {
  const btn = document.getElementById('capture-btn');
  btn.disabled = true;
  btn.textContent = 'Capturing...';
  try {
    await fetch('/api/camera/capture', { method: 'POST' });
    // Wait for cameras to finish capturing (~5s for both)
    await new Promise(r => setTimeout(r, 5000));
    await refreshCameras();
  } catch (err) {
    console.error('Capture failed:', err);
  }
  btn.disabled = false;
  btn.textContent = 'Capture';
}

function refreshCameras() {
  const ts = Date.now();
  loadCameraImage('camera-upper', '/api/camera/upper?t=' + ts);
  loadCameraImage('camera-lower', '/api/camera/lower?t=' + ts);
}

async function loadCameraImage(id, url) {
  const img = document.getElementById(id);
  const tsEl = document.getElementById(id + '-ts');
  try {
    const res = await fetch(url);
    if (!res.ok) throw new Error('not found');
    const blob = await res.blob();
    img.src = URL.createObjectURL(blob);
    img.classList.remove('no-image');
    const taken = res.headers.get('X-Photo-Taken');
    if (taken) {
      const d = new Date(taken);
      tsEl.textContent = d.toLocaleDateString() + ' ' + d.toLocaleTimeString();
    }
  } catch {
    img.classList.add('no-image');
    img.removeAttribute('src');
    img.alt = 'No image available';
    tsEl.textContent = '';
  }
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

  const stageSelect = document.getElementById('stage-select');
  if (stageSelect) {
    stageSelect.value = inferStage(schedule.pump);
  }
}

async function setStage(stageKey) {
  if (stageKey === 'custom') {
    document.getElementById('stage-select').value = inferStage(schedule.pump);
    return;
  }
  const preset = STAGE_PRESETS[stageKey];
  if (!preset) return;

  const updated = {
    location: schedule.location,
    light: schedule.light,
    pump: {
      runs_per_day: preset.runs_per_day,
      run_duration_minutes: preset.run_duration_minutes,
      speed: schedule.pump?.speed ?? 60
    },
    scheduler: schedule.scheduler
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
    } else {
      const msg = await res.text();
      alert('Failed to set stage: ' + msg);
      document.getElementById('stage-select').value = inferStage(schedule.pump);
    }
  } catch (err) {
    alert('Failed to set stage');
    document.getElementById('stage-select').value = inferStage(schedule.pump);
  }
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

// --- Pod Inventory ---
const POD_STAGE_DAYS = { germination: 15, seedling: 29, vegetative: 50 };

function podID(col, row) {
  return String.fromCharCode(65 + col) + (row + 1);
}

function daysSince(dateStr) {
  if (!dateStr) return 0;
  return Math.floor((Date.now() - new Date(dateStr).getTime()) / 86400000);
}

function activePlantingFor(id) {
  return inventory.plantings.find(p => p.pod === id && !p.ended);
}

function plantingHistoryFor(id) {
  return inventory.plantings
    .filter(p => p.pod === id && p.ended)
    .sort((a, b) => b.ended.localeCompare(a.ended));
}

function computeStage(planting) {
  if (!planting) return 'empty';
  const days = daysSince(planting.planted);
  if (planting.days_to_harvest && days > planting.days_to_harvest) return 'harvest_ready';
  if (days < POD_STAGE_DAYS.germination) return 'germination';
  if (days < POD_STAGE_DAYS.seedling) return 'seedling';
  if (days < POD_STAGE_DAYS.vegetative) return 'vegetative';
  return 'flowering';
}

function nextPlantingId() {
  return inventory.plantings.reduce((m, p) => Math.max(m, p.id || 0), 0) + 1;
}

async function loadInventory() {
  try {
    const res = await fetch('/api/plantings');
    if (!res.ok) throw new Error(await res.text());
    inventory = await res.json();
    if (!inventory.plantings) inventory.plantings = [];
    renderPodGrid();
  } catch (err) {
    console.error('Failed to load inventory:', err);
  }
}

function renderPodGrid() {
  const grid = document.getElementById('pod-grid');
  if (!grid) return;

  const { columns, rows } = inventory.layout;
  grid.style.setProperty('--pod-cols', columns);
  grid.innerHTML = '';

  let active = 0;
  let harvestReady = 0;

  for (let row = 0; row < rows; row++) {
    for (let col = 0; col < columns; col++) {
      const id = podID(col, row);
      const planting = activePlantingFor(id);
      const stage = computeStage(planting);

      const cell = document.createElement('div');
      cell.className = 'pod-cell ' + stage;
      cell.onclick = () => openPodModal(id);
      cell.title = planting ? `${planting.variety} (${daysSince(planting.planted)}d)` : `Pod ${id} — empty`;

      const idEl = document.createElement('div');
      idEl.className = 'pod-id';
      idEl.textContent = id;
      cell.appendChild(idEl);

      if (planting) {
        active++;
        if (stage === 'harvest_ready') harvestReady++;

        const variety = document.createElement('div');
        variety.className = 'pod-variety';
        variety.textContent = planting.variety;
        cell.appendChild(variety);

        const age = document.createElement('div');
        age.className = 'pod-age';
        age.textContent = daysSince(planting.planted) + 'd';
        cell.appendChild(age);
      } else {
        cell.classList.add('empty');
      }

      grid.appendChild(cell);
    }
  }

  document.getElementById('pod-stats').textContent =
    `${active} active · ${harvestReady} ready to harvest`;
}

function openPodModal(podId) {
  activePodId = podId;
  document.getElementById('pod-modal-title').textContent = 'Pod ' + podId;

  const planting = activePlantingFor(podId);
  const activeForm = document.getElementById('pod-active-form');
  const emptyForm = document.getElementById('pod-empty-form');

  if (planting) {
    activeForm.classList.remove('hidden');
    emptyForm.classList.add('hidden');
    document.getElementById('edit-variety').value = planting.variety || '';
    document.getElementById('edit-planted').value = planting.planted || '';
    document.getElementById('edit-days').value = planting.days_to_harvest || '';
    document.getElementById('edit-source').value = planting.seed_source || '';
    document.getElementById('edit-notes').value = planting.notes || '';
  } else {
    activeForm.classList.add('hidden');
    emptyForm.classList.remove('hidden');
    document.getElementById('new-variety').value = '';
    document.getElementById('new-planted').value = new Date().toISOString().slice(0, 10);
    document.getElementById('new-days').value = '';
    document.getElementById('new-source').value = '';
    document.getElementById('new-notes').value = '';
  }

  const histEl = document.getElementById('pod-history');
  const history = plantingHistoryFor(podId);
  if (history.length) {
    const rows = history.map(p =>
      `<div class="history-row"><span>${escapeHtml(p.variety)} (${p.planted} → ${p.ended})</span><span class="history-reason">${p.end_reason || '—'}</span></div>`
    ).join('');
    histEl.innerHTML = '<h4>Past plantings</h4>' + rows;
  } else {
    histEl.innerHTML = '';
  }

  document.getElementById('pod-modal').classList.remove('hidden');
}

function closePodModal() {
  document.getElementById('pod-modal').classList.add('hidden');
  activePodId = null;
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

async function addPlanting() {
  if (!activePodId) return;
  const variety = document.getElementById('new-variety').value.trim();
  const planted = document.getElementById('new-planted').value;
  if (!variety) { alert('Variety is required'); return; }
  if (!planted) { alert('Planted date is required'); return; }

  const planting = {
    id: nextPlantingId(),
    pod: activePodId,
    variety,
    planted
  };
  const days = parseInt(document.getElementById('new-days').value);
  if (days > 0) planting.days_to_harvest = days;
  const source = document.getElementById('new-source').value.trim();
  if (source) planting.seed_source = source;
  const notes = document.getElementById('new-notes').value.trim();
  if (notes) planting.notes = notes;

  inventory.plantings.push(planting);
  if (await saveInventory()) {
    closePodModal();
    renderPodGrid();
  } else {
    inventory.plantings.pop();
  }
}

async function savePodEdit() {
  const planting = activePlantingFor(activePodId);
  if (!planting) return;

  const before = { ...planting };
  const variety = document.getElementById('edit-variety').value.trim();
  if (!variety) { alert('Variety is required'); return; }

  planting.variety = variety;
  planting.planted = document.getElementById('edit-planted').value;
  const days = parseInt(document.getElementById('edit-days').value);
  if (days > 0) planting.days_to_harvest = days; else delete planting.days_to_harvest;
  const source = document.getElementById('edit-source').value.trim();
  if (source) planting.seed_source = source; else delete planting.seed_source;
  const notes = document.getElementById('edit-notes').value.trim();
  if (notes) planting.notes = notes; else delete planting.notes;

  if (await saveInventory()) {
    closePodModal();
    renderPodGrid();
  } else {
    Object.assign(planting, before);
  }
}

async function endPlanting(reason) {
  const planting = activePlantingFor(activePodId);
  if (!planting) return;
  if (!confirm(`Mark this planting as ${reason}?`)) return;

  const before = { ...planting };
  planting.ended = new Date().toISOString().slice(0, 10);
  planting.end_reason = reason;

  if (await saveInventory()) {
    closePodModal();
    renderPodGrid();
  } else {
    Object.assign(planting, before);
    delete planting.ended;
    delete planting.end_reason;
    if (before.ended) planting.ended = before.ended;
    if (before.end_reason) planting.end_reason = before.end_reason;
  }
}

async function saveInventory() {
  try {
    const res = await fetch('/api/plantings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(inventory)
    });
    if (!res.ok) {
      alert('Failed to save: ' + (await res.text()));
      return false;
    }
    return true;
  } catch (err) {
    alert('Failed to save inventory');
    return false;
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

  // Group by sensor (convert temperatures C->F for display)
  const groups = {};
  readings.forEach(r => {
    if (!groups[r.s]) groups[r.s] = [];
    const y = (r.s === 'temperature' || r.s === 'pcb_temp') ? cToF(r.v) : r.v;
    groups[r.s].push({ x: new Date(r.t), y });
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
          title: { display: true, text: 'Temperature (°F) / Level' },
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

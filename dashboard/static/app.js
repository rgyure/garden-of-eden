let state = {};
let schedule = {};
let inventory = { layout: { columns: 3, rows: 10 }, plantings: [] };
let varieties = [];
let activePodId = null;
let chart = null;
let harvestChart = null;
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
  loadVarieties();
  loadInventory();
  loadHistory('24h');
  refreshCameras();
  loadPodEvents();
  loadReservoir();
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
  setVal('pump-current', state.pump_current);
  setVal('water-temp-value', cToF(state.water_temp));
  renderWaterTempWarning(state);
  renderNutrients(state);
  renderDosePumps(state);

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

// Reservoir water-temperature warnings:
// > 75°F dissolved oxygen drops sharply -> root rot risk.
// > 80°F is a hard red zone for most hydroponic crops.
function renderWaterTempWarning(state) {
  const card = document.getElementById('water-temp-card');
  if (!card) return;
  const tF = cToF(state.water_temp);
  card.classList.remove('warning', 'danger');
  if (tF === null || tF === undefined) return;
  if (tF >= 80) card.classList.add('danger');
  else if (tF >= 75) card.classList.add('warning');
}

// --- Nutrients + dose pumps ---
function renderNutrients(state) {
  setVal('ph-value', state.ph);
  setVal('ec-value', state.ec);

  // Color the pH/EC cards red when outside the configured target range.
  const ph = state.ph;
  const ec = state.ec;
  const nutrients = (schedule && schedule.nutrients) || {};
  const phRange = nutrients.ph || { target_min: 5.8, target_max: 6.2 };
  const ecRange = nutrients.ec || { target_min: 1.2, target_max: 1.8 };

  document.getElementById('ph-target').textContent =
    `${phRange.target_min}–${phRange.target_max}`;
  document.getElementById('ec-target').textContent =
    `mS/cm · ${ecRange.target_min}–${ecRange.target_max}`;

  const phCard = document.getElementById('ph-card');
  const ecCard = document.getElementById('ec-card');
  phCard.classList.toggle('warning',
    ph !== null && ph !== undefined && (ph < phRange.target_min || ph > phRange.target_max));
  ecCard.classList.toggle('warning',
    ec !== null && ec !== undefined && (ec < ecRange.target_min || ec > ecRange.target_max));
}

function renderDosePumps(state) {
  const doseState = state.dose_state || {};
  const doseLast  = state.dose_last  || {};
  let anyReal = false;

  const pumpCfg = (schedule && schedule.dose_pumps) || {};
  const anomalyMap = state.dose_anomaly || {};
  ['ph_down', 'nutrient_a', 'nutrient_b', 'cal_mag'].forEach(name => {
    const statusEl  = document.getElementById('dose-' + name + '-status');
    const lastEl    = document.getElementById('dose-' + name + '-last');
    const productEl = document.getElementById('dose-' + name + '-product');
    const rowEl     = document.querySelector('.dose-row[data-pump="' + name + '"]');
    const status = doseState[name] || 'IDLE';
    statusEl.textContent = status.toLowerCase();
    statusEl.classList.toggle('running', status === 'RUNNING');
    if (productEl) {
      const product = pumpCfg[name] && pumpCfg[name].product;
      productEl.textContent = product || '';
    }
    // Flag the row if the most recent verification failed.
    if (rowEl) {
      let anomalyOk = true;
      const raw = anomalyMap[name];
      if (raw) {
        try {
          const obj = typeof raw === 'string' ? JSON.parse(raw) : raw;
          anomalyOk = obj && obj.ok !== false;
        } catch (e) { /* ignore */ }
      }
      rowEl.classList.toggle('anomaly', !anomalyOk);
    }

    const lastRaw = doseLast[name];
    if (lastRaw) {
      try {
        const last = typeof lastRaw === 'string' ? JSON.parse(lastRaw) : lastRaw;
        if (last && last.volume_ml !== undefined) {
          lastEl.textContent = `last: ${last.volume_ml} mL` + (last.stub ? ' (stub)' : '');
          if (!last.stub) anyReal = true;
        }
      } catch (e) { /* ignore */ }
    }
  });

  const summary = document.getElementById('dose-summary');
  if (summary) {
    summary.textContent = anyReal
      ? 'Hardware connected'
      : 'Stub mode — hardware not wired';
  }
}

async function doseManual(name) {
  const ml = parseFloat(document.getElementById('dose-' + name + '-ml').value);
  if (!isFinite(ml) || ml <= 0) { alert('Enter a positive mL value.'); return; }
  if (!confirm(`Dose ${ml} mL of ${name}?`)) return;
  try {
    const res = await fetch('/api/dose/' + name, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ volume_ml: ml }),
    });
    if (!res.ok) alert('Dose request failed: ' + (await res.text()));
  } catch (err) {
    alert('Dose request failed');
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
    // Re-render dose pumps so the product names from schedule.yml appear
    // immediately rather than on the next SSE tick.
    renderDosePumps(state);
    renderNutrients(state);
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
    renderHarvestAnalytics();
  } catch (err) {
    console.error('Failed to load inventory:', err);
  }
}

// --- Variety library ---
async function loadVarieties() {
  try {
    const res = await fetch('/api/varieties');
    if (!res.ok) throw new Error(await res.text());
    const data = await res.json();
    varieties = data.varieties || [];
    const list = document.getElementById('variety-options');
    if (list) {
      list.innerHTML = varieties
        .map(v => `<option value="${escapeHtml(v.name)}">`)
        .join('');
    }
  } catch (err) {
    console.error('Failed to load varieties:', err);
  }
}

function findVariety(name) {
  if (!name) return null;
  const target = name.trim().toLowerCase();
  return varieties.find(v => v.name.toLowerCase() === target) || null;
}

// When a known variety is selected, pre-fill days_to_harvest if the user
// hasn't already entered a value. Never overwrites a non-empty field.
function applyVarietyDefaults(prefix) {
  const varietyEl = document.getElementById(prefix + '-variety');
  const daysEl = document.getElementById(prefix + '-days');
  if (!varietyEl || !daysEl) return;
  const match = findVariety(varietyEl.value);
  if (!match) return;
  if (!daysEl.value && match.days_to_harvest) {
    daysEl.value = match.days_to_harvest;
  }
}

// --- Harvest analytics ---
function totalHarvestGrams(planting) {
  if (!planting.harvest_log) return 0;
  return planting.harvest_log.reduce((sum, h) => sum + (h.weight_g || 0), 0);
}

function renderHarvestAnalytics() {
  const plantings = inventory.plantings || [];
  const active = plantings.filter(p => !p.ended);
  const completed = plantings.filter(p => p.ended);
  const harvested = completed.filter(p => p.end_reason === 'harvested');

  const totalGrams = plantings.reduce((sum, p) => sum + totalHarvestGrams(p), 0);
  const varietyNames = new Set(plantings.map(p => p.variety));
  const successRate = completed.length
    ? Math.round((harvested.length / completed.length) * 100) + '%'
    : '--';

  setText('stat-total-g', totalGrams.toFixed(0) + ' g');
  setText('stat-active', active.length);
  setText('stat-completed', completed.length);
  setText('stat-success', successRate);
  setText('stat-varieties', varietyNames.size);
  setText('harvest-summary', `${plantings.length} planting${plantings.length === 1 ? '' : 's'} on record`);

  renderYieldChart(plantings);
  renderReadySoon(active);
}

function renderYieldChart(plantings) {
  const canvas = document.getElementById('yield-chart');
  const emptyMsg = document.getElementById('yield-empty');
  if (!canvas || !emptyMsg) return;

  const yieldByVariety = {};
  plantings.forEach(p => {
    const g = totalHarvestGrams(p);
    if (g > 0) yieldByVariety[p.variety] = (yieldByVariety[p.variety] || 0) + g;
  });

  const entries = Object.entries(yieldByVariety).sort((a, b) => b[1] - a[1]);
  if (!entries.length) {
    canvas.classList.add('hidden');
    emptyMsg.classList.remove('hidden');
    if (harvestChart) { harvestChart.destroy(); harvestChart = null; }
    return;
  }
  canvas.classList.remove('hidden');
  emptyMsg.classList.add('hidden');

  const labels = entries.map(([v]) => v);
  const data = entries.map(([, g]) => g);

  if (harvestChart) harvestChart.destroy();
  harvestChart = new Chart(canvas.getContext('2d'), {
    type: 'bar',
    data: {
      labels,
      datasets: [{
        label: 'Grams harvested',
        data,
        backgroundColor: '#4caf50',
        borderRadius: 4
      }]
    },
    options: {
      indexAxis: 'y',
      responsive: true,
      maintainAspectRatio: false,
      plugins: { legend: { display: false } },
      scales: {
        x: { title: { display: true, text: 'Grams' }, grid: { color: '#f0f0f0' } },
        y: { grid: { display: false } }
      }
    }
  });
}

function renderReadySoon(activePlantings) {
  const el = document.getElementById('ready-soon');
  if (!el) return;

  const upcoming = activePlantings
    .filter(p => p.days_to_harvest)
    .map(p => {
      const days = daysSince(p.planted);
      const remaining = p.days_to_harvest - days;
      return { planting: p, remaining };
    })
    .filter(x => x.remaining <= 14)
    .sort((a, b) => a.remaining - b.remaining);

  if (!upcoming.length) {
    el.innerHTML = '';
    return;
  }

  const rows = upcoming.map(({ planting, remaining }) => {
    const label = remaining <= 0
      ? `ready (${Math.abs(remaining)}d past target)`
      : `${remaining}d to go`;
    const cls = remaining <= 0 ? 'ready-now' : 'ready-soon';
    return `<div class="ready-row ${cls}"><span class="ready-pod">${planting.pod}</span><span class="ready-variety">${escapeHtml(planting.variety)}</span><span class="ready-eta">${label}</span></div>`;
  }).join('');

  el.innerHTML = '<h4 class="modal-section-title">Ready soon</h4>' + rows;
}

function setText(id, value) {
  const el = document.getElementById(id);
  if (el) el.textContent = value;
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
    document.getElementById('harvest-grams').value = '';
    document.getElementById('harvest-notes').value = '';
    renderHarvestLogForActive(planting);
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

function renderHarvestLogForActive(planting) {
  const el = document.getElementById('harvest-log-list');
  if (!el) return;
  const log = planting.harvest_log || [];
  if (!log.length) { el.innerHTML = ''; return; }
  const rows = log
    .slice()
    .sort((a, b) => b.date.localeCompare(a.date))
    .map(h => {
      const grams = h.weight_g ? `${h.weight_g.toFixed(1)} g` : '';
      const notes = h.notes ? ` — ${escapeHtml(h.notes)}` : '';
      return `<div class="history-row"><span>${h.date}</span><span>${grams}${notes}</span></div>`;
    })
    .join('');
  el.innerHTML = rows;
}

async function logHarvest() {
  const planting = activePlantingFor(activePodId);
  if (!planting) return;
  const gramsRaw = document.getElementById('harvest-grams').value.trim();
  if (!gramsRaw) { alert('Enter a weight in grams.'); return; }
  const grams = parseFloat(gramsRaw);
  if (!isFinite(grams) || grams < 0) { alert('Weight must be a positive number.'); return; }

  const entry = {
    date: new Date().toISOString().slice(0, 10),
    weight_g: grams
  };
  const notes = document.getElementById('harvest-notes').value.trim();
  if (notes) entry.notes = notes;

  const before = planting.harvest_log ? planting.harvest_log.slice() : null;
  planting.harvest_log = (planting.harvest_log || []).concat(entry);

  if (await saveInventory()) {
    document.getElementById('harvest-grams').value = '';
    document.getElementById('harvest-notes').value = '';
    renderHarvestLogForActive(planting);
    renderHarvestAnalytics();
  } else {
    if (before === null) delete planting.harvest_log;
    else planting.harvest_log = before;
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
    water_level:  { label: 'Water Level',  color: '#00bcd4', yAxisID: 'y' },
    pump_current: { label: 'Pump Current', color: '#9c27b0', yAxisID: 'y2' }
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
        },
        y2: {
          position: 'right',
          offset: true,
          title: { display: true, text: 'Pump current (mA)' },
          ticks: { font: { size: 11 } },
          grid: { drawOnChartArea: false }
        }
      },
      plugins: {
        legend: { position: 'bottom', labels: { boxWidth: 12, font: { size: 12 } } }
      }
    }
  });
}

// --- Pod calibration + detection ---
// Upper camera covers the top 15 pods (rows 1-5); lower covers rows 6-10.
const CALIB_POD_ORDER = {
  upper: ['A1','B1','C1','A2','B2','C2','A3','B3','C3','A4','B4','C4','A5','B5','C5'],
  lower: ['A6','B6','C6','A7','B7','C7','A8','B8','C8','A9','B9','C9','A10','B10','C10']
};

let calibCamera = 'upper';
let calibration = { upper: { positions: [] }, lower: { positions: [] } };
let calibPoints = []; // working copy for the active camera
let calibImgSize = { width: 0, height: 0 };

function openCalibrationModal() {
  loadCalibration().then(() => {
    switchCalibCamera('upper');
    document.getElementById('calibration-modal').classList.remove('hidden');
  });
}

function closeCalibrationModal() {
  document.getElementById('calibration-modal').classList.add('hidden');
}

async function loadCalibration() {
  try {
    const res = await fetch('/api/pod-calibration');
    if (!res.ok) throw new Error(await res.text());
    const data = await res.json();
    calibration = {
      upper: data.upper || { positions: [] },
      lower: data.lower || { positions: [] }
    };
    if (!calibration.upper.positions) calibration.upper.positions = [];
    if (!calibration.lower.positions) calibration.lower.positions = [];
  } catch (err) {
    console.error('Failed to load calibration:', err);
  }
}

function switchCalibCamera(cam) {
  // Save the working copy back to the right side before switching.
  if (calibPoints.length || calibImgSize.width) {
    calibration[calibCamera].positions = calibPoints.slice();
    if (calibImgSize.width) {
      calibration[calibCamera].width = calibImgSize.width;
      calibration[calibCamera].height = calibImgSize.height;
    }
  }
  calibCamera = cam;
  calibPoints = (calibration[cam].positions || []).slice();
  document.getElementById('calib-camera-label').textContent = '— ' + cam;
  document.getElementById('calib-tab-upper').classList.toggle('btn-primary', cam === 'upper');
  document.getElementById('calib-tab-lower').classList.toggle('btn-primary', cam === 'lower');

  const img = document.getElementById('calib-image');
  img.src = `/api/camera/${cam}?t=${Date.now()}`;
  img.onload = () => {
    calibImgSize = { width: img.naturalWidth, height: img.naturalHeight };
    setupCalibOverlay();
    redrawCalibPoints();
    updateCalibStatus();
  };
  img.onclick = onCalibClick;
}

function setupCalibOverlay() {
  const svg = document.getElementById('calib-overlay');
  svg.setAttribute('viewBox', `0 0 ${calibImgSize.width} ${calibImgSize.height}`);
  svg.onclick = onCalibClick;
}

function onCalibClick(ev) {
  const img = document.getElementById('calib-image');
  const rect = img.getBoundingClientRect();
  const scaleX = calibImgSize.width / rect.width;
  const scaleY = calibImgSize.height / rect.height;
  const x = Math.round((ev.clientX - rect.left) * scaleX);
  const y = Math.round((ev.clientY - rect.top) * scaleY);
  const r = parseInt(document.getElementById('calib-radius').value, 10);

  const order = CALIB_POD_ORDER[calibCamera];
  const nextIdx = calibPoints.length;
  if (nextIdx >= order.length) return; // all placed

  calibPoints.push({ pod: order[nextIdx], x, y, radius: r });
  redrawCalibPoints();
  updateCalibStatus();
}

function redrawCalibPoints() {
  const svg = document.getElementById('calib-overlay');
  svg.innerHTML = calibPoints.map(p => `
    <circle cx="${p.x}" cy="${p.y}" r="${p.radius}"
            fill="rgba(76,175,80,0.25)" stroke="#2e7d32" stroke-width="3"/>
    <text x="${p.x}" y="${p.y + 6}" text-anchor="middle"
          font-size="${Math.max(14, p.radius * 0.6)}" fill="#1b5e20" font-weight="600">${p.pod}</text>
  `).join('');
}

function updateCalibStatus() {
  const order = CALIB_POD_ORDER[calibCamera];
  const placed = calibPoints.length;
  const total = order.length;
  document.getElementById('calib-progress').textContent = `${placed} of ${total} placed`;
  document.getElementById('calib-next-pod').textContent = placed < total ? `Next: ${order[placed]}` : 'All pods placed';
}

function undoCalibPoint() {
  calibPoints.pop();
  redrawCalibPoints();
  updateCalibStatus();
}

function clearCalibPoints() {
  if (!confirm('Clear all placed pods for ' + calibCamera + '?')) return;
  calibPoints = [];
  redrawCalibPoints();
  updateCalibStatus();
}

async function saveCalibration() {
  // Persist the working copy back to the active camera.
  calibration[calibCamera].positions = calibPoints.slice();
  if (calibImgSize.width) {
    calibration[calibCamera].width = calibImgSize.width;
    calibration[calibCamera].height = calibImgSize.height;
  }
  try {
    const res = await fetch('/api/pod-calibration', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(calibration)
    });
    if (!res.ok) { alert('Failed to save: ' + (await res.text())); return; }
    closeCalibrationModal();
    loadPodEvents();
  } catch (err) {
    alert('Failed to save calibration');
  }
}

async function captureBaseline() {
  if (!confirm(`Snapshot the current ${calibCamera} frame as the baseline? Future scans compare against this image.`)) return;
  try {
    const res = await fetch(`/api/pod-baseline/${calibCamera}`, { method: 'POST' });
    if (!res.ok) { alert('Failed to capture baseline: ' + (await res.text())); return; }
    alert(`${calibCamera} baseline saved.`);
  } catch (err) {
    alert('Failed to capture baseline');
  }
}

async function runPodScan() {
  const btn = document.getElementById('scan-btn');
  btn.disabled = true;
  btn.textContent = 'Scanning...';
  try {
    const res = await fetch('/api/pod-scan', { method: 'POST' });
    const data = await res.json();
    renderScanResults(data.results || []);
    loadPodEvents();
  } catch (err) {
    alert('Scan failed');
  } finally {
    btn.disabled = false;
    btn.textContent = 'Scan now';
  }
}

function renderScanResults(results) {
  const summary = document.getElementById('pod-events-summary');
  const errors = results.filter(r => r.error);
  if (errors.length) {
    summary.innerHTML = errors.map(r => `<div class="scan-error">${escapeHtml(r.camera)}: ${escapeHtml(r.error)}</div>`).join('');
    return;
  }
  const changed = results.flatMap(r => r.events.filter(e => e.changed));
  summary.textContent = changed.length
    ? `Last scan: ${changed.length} pod${changed.length === 1 ? '' : 's'} changed.`
    : 'Last scan: no changes detected.';
}

async function loadPodEvents() {
  try {
    const res = await fetch('/api/pod-events');
    if (!res.ok) throw new Error(await res.text());
    const data = await res.json();
    renderPodEvents(data.events || []);
  } catch (err) {
    console.error('Failed to load pod events:', err);
  }
}

function renderPodEvents(events) {
  const el = document.getElementById('pod-events-list');
  const summary = document.getElementById('pod-events-summary');
  if (!events.length) {
    el.innerHTML = '';
    if (summary.textContent.startsWith('Not calibrated')) return;
    return;
  }
  const rows = events.slice(0, 20).map(e => {
    const ts = new Date(e.date);
    const when = isFinite(ts) ? ts.toLocaleString() : e.date;
    return `<div class="event-row">
      <span class="event-pod">${e.pod}</span>
      <span class="event-camera">${e.camera}</span>
      <span class="event-when">${when}</span>
      <span class="event-mag">Δ ${e.magnitude.toFixed(1)}</span>
    </div>`;
  }).join('');
  el.innerHTML = '<h4 class="modal-section-title">Recent change events</h4>' + rows;
}

// --- Reservoir age tracker ---
// Recommended cadence for a 7-8L Gardyn reservoir: full change every
// 7-10 days vegetative, every 14 days fruiting. We color-code at 7+ amber,
// 10+ red.
async function loadReservoir() {
  try {
    const res = await fetch('/api/reservoir');
    if (!res.ok) throw new Error(await res.text());
    renderReservoir(await res.json());
  } catch (err) {
    console.error('Failed to load reservoir state:', err);
  }
}

function renderReservoir(rs) {
  const valueEl   = document.getElementById('reservoir-age-value');
  const card      = document.getElementById('reservoir-card');
  const statsEl   = document.getElementById('reservoir-stats');
  const lastEl    = document.getElementById('reservoir-last');
  const historyEl = document.getElementById('reservoir-history');
  if (!valueEl || !card) return;

  const days = rs.days_since_change;
  valueEl.textContent = days < 0 ? '--' : days;
  card.classList.remove('warning', 'danger');
  if (days >= 10) card.classList.add('danger');
  else if (days >= 7) card.classList.add('warning');

  if (statsEl) {
    if (days < 0) {
      statsEl.textContent = 'Never recorded';
    } else if (days >= 10) {
      statsEl.textContent = 'Overdue — change soon';
    } else if (days >= 7) {
      statsEl.textContent = 'Due for a change this week';
    } else {
      statsEl.textContent = 'Healthy';
    }
  }

  if (lastEl) {
    if (rs.last_change) {
      const noteText = rs.notes ? ` — ${escapeHtml(rs.notes)}` : '';
      lastEl.innerHTML = `Last change: <strong>${rs.last_change}</strong>${noteText}`;
    } else {
      lastEl.textContent = 'No change recorded yet. Press "I changed it today" once you do a flush.';
    }
  }

  if (historyEl) {
    const items = (rs.history || []).slice(0, 20).map(h => {
      const noteText = h.notes ? ` — ${escapeHtml(h.notes)}` : '';
      return `<div class="history-row"><span>${h.date}</span><span>${noteText}</span></div>`;
    }).join('');
    historyEl.innerHTML = items || '<div class="history-row"><span>None yet</span></div>';
  }
}

async function markReservoirChanged() {
  const notes = (document.getElementById('reservoir-notes').value || '').trim();
  if (!confirm('Record that the reservoir was fully changed today?')) return;
  try {
    const res = await fetch('/api/reservoir/change', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ notes }),
    });
    if (!res.ok) {
      alert('Failed to record change: ' + (await res.text()));
      return;
    }
    document.getElementById('reservoir-notes').value = '';
    loadReservoir();
  } catch (err) {
    alert('Failed to record change');
  }
}

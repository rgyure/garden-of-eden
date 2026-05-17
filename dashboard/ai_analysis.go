package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// AnalysisIssue is one observation Claude flagged about the garden.
type AnalysisIssue struct {
	Severity    string `json:"severity"`
	Pod         string `json:"pod"`
	Description string `json:"description"`
}

// PlantingAssessment is the per-pod detailed write-up.
type PlantingAssessment struct {
	Pod                  string   `json:"pod"`
	Variety              string   `json:"variety"`
	GrowthStage          string   `json:"growth_stage"`
	DaysPlanted          int      `json:"days_planted"`
	Assessment           string   `json:"assessment"`
	ProjectedHarvestDate string   `json:"projected_harvest_date,omitempty"`
	YieldEstimateG       float64  `json:"yield_estimate_g,omitempty"`
	ActionsToday         []string `json:"actions_today,omitempty"`
}

// InventoryDiscrepancy compares logged plantings against what is visible.
type InventoryDiscrepancy struct {
	Type        string `json:"type"`        // logged_empty | unlogged_planting | variety_mismatch
	Pod         string `json:"pod"`
	Description string `json:"description"`
}

// DayPlan is one entry in the seven-day forward plan.
type DayPlan struct {
	Day     string   `json:"day"`             // Mon, Tue, ... or YYYY-MM-DD
	Actions []string `json:"actions"`
}

// RiskItem describes a forecasted concern.
type RiskItem struct {
	Risk       string `json:"risk"`
	Likelihood string `json:"likelihood"` // low | medium | high
	Rationale  string `json:"rationale"`
	Mitigation string `json:"mitigation"`
}

// AnalysisResult is the structured response we expect from Claude.
type AnalysisResult struct {
	Date                     string                 `json:"date"`
	OverallHealth            string                 `json:"overall_health"`
	ExecutiveSummary         string                 `json:"executive_summary"`
	CanopyAssessment         string                 `json:"canopy_assessment,omitempty"`
	RootZoneAssessment       string                 `json:"root_zone_assessment,omitempty"`
	EnvironmentalAssessment  string                 `json:"environmental_assessment,omitempty"`
	HarvestOutlook           string                 `json:"harvest_outlook,omitempty"`
	PerPlanting              []PlantingAssessment   `json:"per_planting,omitempty"`
	InventoryDiscrepancies   []InventoryDiscrepancy `json:"inventory_discrepancies,omitempty"`
	SevenDayPlan             []DayPlan              `json:"seven_day_plan,omitempty"`
	RiskForecast             []RiskItem             `json:"risk_forecast,omitempty"`
	Issues                   []AnalysisIssue        `json:"issues,omitempty"`
	HarvestReady             []string               `json:"harvest_ready,omitempty"`
	NeedsAttention           []string               `json:"needs_attention,omitempty"`
	Recommendations          []string               `json:"recommendations,omitempty"`
	// Back-compat with the original v1 prompt; renderer still reads this if present.
	Summary       string `json:"summary,omitempty"`
	Model         string `json:"model,omitempty"`
	InputTokens   int    `json:"input_tokens,omitempty"`
	OutputTokens  int    `json:"output_tokens,omitempty"`
	Error         string `json:"error,omitempty"`
}

const (
	anthropicURL     = "https://api.anthropic.com/v1/messages"
	anthropicVersion = "2023-06-01"
	defaultAIModel   = "claude-opus-4-7"
	aiRequestTimeout = 120 * time.Second
)

var aiAnalysisMu sync.Mutex

// startDailyAIAnalysis schedules one Claude analysis per day at the same
// hour as the pod-occupancy scan. The first run happens once that hour
// arrives; on boot we don't fire immediately.
func (app *App) startDailyAIAnalysis(hour int) {
	if !app.config.AIAnalysisEnabled {
		log.Printf("AI analysis disabled (set AI_ANALYSIS_ENABLED=true and ANTHROPIC_API_KEY to enable)")
		return
	}
	if app.config.AnthropicAPIKey == "" {
		log.Printf("AI analysis enabled but ANTHROPIC_API_KEY is empty; daily analysis will not run")
		return
	}
	go func() {
		for {
			next := nextLocalTime(hour)
			log.Printf("Next AI garden analysis scheduled for %s", next.Format(time.RFC1123))
			time.Sleep(time.Until(next))
			if _, err := app.runAIAnalysis(); err != nil {
				log.Printf("daily AI analysis: %v", err)
			}
		}
	}()
}

func (app *App) handleRunAIAnalysis(w http.ResponseWriter, r *http.Request) {
	if !app.config.AIAnalysisEnabled {
		http.Error(w, "AI analysis disabled. Set AI_ANALYSIS_ENABLED=true.", http.StatusServiceUnavailable)
		return
	}
	if app.config.AnthropicAPIKey == "" {
		http.Error(w, "ANTHROPIC_API_KEY not configured", http.StatusServiceUnavailable)
		return
	}
	result, err := app.runAIAnalysis()
	if err != nil {
		http.Error(w, "analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (app *App) handleGetAIAnalysis(w http.ResponseWriter, r *http.Request) {
	results, err := readAIAnalysisLog(app.config.AIAnalysisPath, 30)
	if err != nil {
		http.Error(w, "failed to read analysis log: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"results": results,
		"enabled": app.config.AIAnalysisEnabled && app.config.AnthropicAPIKey != "",
	})
}

// runAIAnalysis triggers a fresh camera capture, loads the two JPEGs, asks
// Claude to analyze them, and appends the structured result to the log.
func (app *App) runAIAnalysis() (*AnalysisResult, error) {
	aiAnalysisMu.Lock()
	defer aiAnalysisMu.Unlock()

	if app.mqtt != nil {
		app.mqtt.Publish(app.config.BaseTopic+"/image/capture", "")
	}
	time.Sleep(7 * time.Second)

	upper, err := os.ReadFile(app.config.UpperImagePath)
	if err != nil {
		return nil, fmt.Errorf("read upper image: %w", err)
	}
	lower, err := os.ReadFile(app.config.LowerImagePath)
	if err != nil {
		return nil, fmt.Errorf("read lower image: %w", err)
	}

	prompt := app.buildAnalysisPrompt()
	result, err := callAnthropic(
		app.config.AnthropicAPIKey,
		app.config.AIAnalysisModel,
		prompt,
		upper, lower,
	)
	if err != nil {
		failure := &AnalysisResult{
			Date:  time.Now().UTC().Format(time.RFC3339),
			Error: err.Error(),
		}
		_ = appendAIAnalysisLog(app.config.AIAnalysisPath, failure)
		return failure, err
	}
	result.Date = time.Now().UTC().Format(time.RFC3339)
	if err := appendAIAnalysisLog(app.config.AIAnalysisPath, result); err != nil {
		log.Printf("AI analysis: failed to persist result: %v", err)
	}
	return result, nil
}

// buildAnalysisPrompt fills the system context with live state + inventory.
func (app *App) buildAnalysisPrompt() string {
	snap := app.state.Snapshot()
	rs, _ := readReservoir(app.config.ReservoirPath)
	inv, _ := readInventory(app.config.PodsPath)

	phRange := "5.8-6.2"
	ecRange := "1.2-1.8"
	if sched, err := readScheduleForPrompt(app.config.SchedulePath); err == nil && sched != nil && sched.Nutrients != nil {
		if sched.Nutrients.PH != nil {
			phRange = fmt.Sprintf("%.2f-%.2f", sched.Nutrients.PH.TargetMin, sched.Nutrients.PH.TargetMax)
		}
		if sched.Nutrients.EC != nil {
			ecRange = fmt.Sprintf("%.2f-%.2f", sched.Nutrients.EC.TargetMin, sched.Nutrients.EC.TargetMax)
		}
	}

	plantingList := "(no active plantings recorded)"
	if inv != nil {
		var lines []string
		now := time.Now()
		for _, p := range inv.Plantings {
			if p.Ended != "" {
				continue
			}
			days := 0
			if t, err := time.Parse(dateFormat, p.Planted); err == nil {
				days = int(now.Sub(t).Hours() / 24)
			}
			lines = append(lines, fmt.Sprintf("  - %s -> %s (planted %dd ago, target harvest %dd)",
				p.Pod, p.Variety, days, p.DaysToHarvest))
		}
		if len(lines) > 0 {
			plantingList = ""
			for _, l := range lines {
				plantingList += l + "\n"
			}
		}
	}

	daysSinceChange := -1
	if rs != nil {
		daysSinceChange = daysSinceLocal(rs.LastChange)
	}

	waterTempF := "n/a"
	if snap.WaterTemp != nil {
		waterTempF = fmt.Sprintf("%.1f", *snap.WaterTemp*9/5+32)
	}
	phStr := "n/a"
	if snap.PH != nil {
		phStr = fmt.Sprintf("%.2f", *snap.PH)
	}
	ecStr := "n/a"
	if snap.EC != nil {
		ecStr = fmt.Sprintf("%.2f", *snap.EC)
	}

	return fmt.Sprintf(`You are a master horticulturalist with deep expertise in hydroponic
food production, performing a rigorous daily clinical assessment of a
Gardyn 3.0 indoor vertical garden. Write at the level of detail you would
use briefing a fellow professional - precise, observation-driven, and
actionable. Avoid generic platitudes. Cite specific pod IDs and visual
evidence.

SYSTEM LAYOUT
- 30 pods in a 3-column x 10-row vertical tower.
- Columns are A (left), B (center), C (right); rows are 1 (top) through 10 (bottom).
- The FIRST image is the UPPER camera, covering pods A1-C5 (rows 1-5).
- The SECOND image is the LOWER camera, covering pods A6-C10 (rows 6-10).
- Pod IDs are column then row, e.g. B3 = second column, third row.

LIVE METRICS
- Days since last full reservoir change: %d
- pH: %s (target %s)
- EC: %s mS/cm (target %s)
- Water temperature: %s F  (>75F = low dissolved O2 risk, >80F = root rot zone)

LOGGED ACTIVE PLANTINGS (pod -> variety, days since planted, target harvest day):
%s

ASSESSMENT REQUIREMENTS

1. Cross-check the inventory against what the cameras actually show.
   For every logged planting, confirm a plant is visible in roughly the
   right pod; for every visible plant, confirm there is a matching log
   entry. Catch the obvious (pod recorded as Basil but you see clearly
   a tomato; pod recorded as occupied but visually empty; plants
   visible but no inventory record).

2. Write a full clinical report (~600 words across the multi-section
   fields below). Speak as a horticulturalist would: name growth stages,
   call out specific symptoms (interveinal chlorosis, leggy growth,
   tip burn, calcium streaking, etc.), and link environmental metrics
   to plant condition.

3. For every logged active planting, produce a dedicated detailed
   assessment paragraph including: growth stage, visible condition,
   variety-specific care for the coming week (e.g. "top Genovese basil
   once it reaches 6 inches to drive bushiness"), projected harvest
   date based on observed growth vs. the variety's typical timeline,
   and a yield estimate in grams.

4. Build a concrete 7-day action plan, one entry per day starting
   today. Each entry should list 1-3 actions, e.g. "Tuesday: top basil
   pod A2; harvest lettuce in pod C7 by midday; recalibrate EZO-pH
   if drift continues."

5. Forecast risks given current temperature, humidity, growth stages,
   reservoir age, and pH/EC trends. Common risks: Pythium root rot
   above 75F, powdery mildew at high humidity, calcium lockout at
   high pH, tipburn in lettuce under high transpiration. Rate each
   risk as low / medium / high with rationale and mitigation.

OUTPUT FORMAT

Respond with strict JSON only - no markdown fences, no prose outside
the JSON. Use this exact schema (fields marked optional may be omitted
when not applicable, but prefer to fill everything in):

{
  "overall_health": "thriving" | "healthy" | "stressed" | "concerning",
  "executive_summary": "3-5 sentence top-level brief that a busy operator could read in 20 seconds",
  "canopy_assessment": "paragraph describing leaf color, density, growth habit, flowering, fruiting, pest evidence across the canopy",
  "root_zone_assessment": "paragraph interpreting pH, EC, water temp, and reservoir age as a proxy for root health; tie to any visible deficiencies",
  "environmental_assessment": "paragraph on light coverage, airflow signs (leaning, etiolation), humidity stress signs, and what the metrics together imply",
  "harvest_outlook": "paragraph projecting the next two weeks of harvests with confidence levels",
  "per_planting": [
    {
      "pod": "A1",
      "variety": "Genovese Basil",
      "growth_stage": "germination | seedling | vegetative | flowering | fruiting | mature | harvest_ready",
      "days_planted": 14,
      "assessment": "detailed paragraph specific to this pod and variety",
      "projected_harvest_date": "YYYY-MM-DD",
      "yield_estimate_g": 45,
      "actions_today": ["specific actions for this pod today"]
    }
  ],
  "inventory_discrepancies": [
    {
      "type": "logged_empty" | "unlogged_planting" | "variety_mismatch",
      "pod": "B3",
      "description": "what the camera shows vs. what is logged"
    }
  ],
  "seven_day_plan": [
    {"day": "Day 1 (today)",     "actions": ["..."]},
    {"day": "Day 2 (tomorrow)",  "actions": ["..."]}
  ],
  "risk_forecast": [
    {
      "risk": "Pythium root rot",
      "likelihood": "low" | "medium" | "high",
      "rationale": "why this is the level right now",
      "mitigation": "what to do preventively"
    }
  ],
  "issues": [
    {"severity": "info" | "warn" | "urgent", "pod": "A1" or null, "description": "..."}
  ],
  "harvest_ready":   ["pod IDs ready to harvest in the next 1-3 days"],
  "needs_attention": ["pod IDs with visible problems"],
  "recommendations": ["3-7 highest-priority actions for the operator today"]
}

GUIDELINES
- Reserve "urgent" for same-day intervention (wilting, mold, pest infestation).
- Prefer "warn" for trending issues; use "info" for observations only.
- If the photos are too dim to assess detail, say so explicitly in the
  relevant section rather than guessing. Note "limited by lighting" in
  the assessment.
- If a pod has no logged planting AND no visible plant, omit it entirely.
- Be precise about pod IDs whenever you can localize an observation.`,
		daysSinceChange, phStr, phRange, ecStr, ecRange, waterTempF, plantingList)
}

func readScheduleForPrompt(path string) (*Schedule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Schedule
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// callAnthropic makes the multimodal request to the Anthropic Messages API.
func callAnthropic(apiKey, model, prompt string, upperJPEG, lowerJPEG []byte) (*AnalysisResult, error) {
	if model == "" {
		model = defaultAIModel
	}

	type imgSource struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	}
	type contentBlock struct {
		Type   string     `json:"type"`
		Text   string     `json:"text,omitempty"`
		Source *imgSource `json:"source,omitempty"`
	}
	type message struct {
		Role    string         `json:"role"`
		Content []contentBlock `json:"content"`
	}
	body := struct {
		Model     string    `json:"model"`
		MaxTokens int       `json:"max_tokens"`
		Messages  []message `json:"messages"`
	}{
		Model:     model,
		MaxTokens: 6144,
		Messages: []message{{
			Role: "user",
			Content: []contentBlock{
				{Type: "image", Source: &imgSource{Type: "base64", MediaType: "image/jpeg", Data: base64.StdEncoding.EncodeToString(upperJPEG)}},
				{Type: "image", Source: &imgSource{Type: "base64", MediaType: "image/jpeg", Data: base64.StdEncoding.EncodeToString(lowerJPEG)}},
				{Type: "text", Text: prompt},
			},
		}},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", anthropicURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	client := &http.Client{Timeout: aiRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("anthropic %d: %s", resp.StatusCode, string(respBody))
	}

	var wire struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Model string `json:"model"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &wire); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	var rawText string
	for _, c := range wire.Content {
		if c.Type == "text" {
			rawText += c.Text
		}
	}
	if rawText == "" {
		return nil, errors.New("empty response from anthropic")
	}

	parsed, err := parseAnalysisJSON(rawText)
	if err != nil {
		return nil, fmt.Errorf("parse model JSON: %w (raw: %q)", err, rawText)
	}
	parsed.Model = wire.Model
	parsed.InputTokens = wire.Usage.InputTokens
	parsed.OutputTokens = wire.Usage.OutputTokens
	return parsed, nil
}

func parseAnalysisJSON(s string) (*AnalysisResult, error) {
	b := []byte(s)
	start := bytes.IndexByte(b, '{')
	end := bytes.LastIndexByte(b, '}')
	if start < 0 || end <= start {
		return nil, errors.New("no JSON object found")
	}
	var r AnalysisResult
	if err := json.Unmarshal(b[start:end+1], &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func appendAIAnalysisLog(path string, r *AnalysisResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(r)
}

func readAIAnalysisLog(path string, limit int) ([]AnalysisResult, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return []AnalysisResult{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	results := []AnalysisResult{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var r AnalysisResult
		if err := json.Unmarshal(line, &r); err == nil {
			results = append(results, r)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

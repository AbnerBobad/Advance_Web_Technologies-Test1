// Command experiment is the Section 4 measurement client for ImageLab
// Version 2. It drives the observation API exactly like frontend/app.js:
//
//   - short mode: one prompt GET immediately, then one every second;
//   - long mode: one prompt GET to establish a baseline, then after/wait
//     requests, each followed immediately by the next.
//
// The browser's Date.now() is replaced by Go's time.Now() (both wall clock);
// every request and receipt is timestamped so completion-detection delay can
// be compared across conditions and modes. Detection delay is the client
// receipt time of the terminal snapshot minus the server's post-commit
// wall-clock log time for the same job, joined by job id and version.
//
// It appends one row per run to a CSV and writes a JSONL raw log per run.
package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// snapshot mirrors the public job representation returned by the API.
type snapshot struct {
	Status            string          `json:"status"`
	Version           int             `json:"version"`
	Stage             *string         `json:"stage"`
	VariantsCompleted int             `json:"variants_completed"`
	SimulationStep    *int            `json:"simulation_step"`
	QueuedAt          string          `json:"queued_at"`
	StartedAt         *string         `json:"started_at"`
	CompletedAt       *string         `json:"completed_at"`
	FailedAt          *string         `json:"failed_at"`
	Error             *string         `json:"error"`
	Variants          json.RawMessage `json:"variants"`
}

// accept mirrors the 202 upload response.
type accept struct {
	JobID     string `json:"job_id"`
	ImageID   string `json:"image_id"`
	Status    string `json:"status"`
	StatusURL string `json:"status_url"`
	Version   int    `json:"version"`
}

// event is one line of the per-run JSONL raw log.
type event struct {
	At      string `json:"at"`
	Run     string `json:"run"`
	Event   string `json:"event"`
	Seq     int    `json:"seq,omitempty"`
	Mode    string `json:"mode,omitempty"`
	HTTP    int    `json:"http,omitempty"`
	Version int    `json:"version,omitempty"`
	Status  string `json:"status,omitempty"`
	Note    string `json:"note,omitempty"`
}

type config struct {
	base             string
	image            string
	condition        string
	label            string
	mode             string
	reps             int
	latencyMS        int
	quietDelayMS     int
	simulateDurMS    int
	simulateIntMS    int
	serverLog        string
	csvPath          string
	rawDir           string
	shortIntervalMS  int
	longWaitSeconds  int
	requestTimeoutMS int
}

type result struct {
	runID          string
	jobID          string
	image          string
	condition      string
	label          string
	mode           string
	rep            int
	latencySetting string
	quietDelayMS   int
	simulateDurMS  int
	simulateIntMS  int
	latencyMS      int
	uploadAckMS    float64
	promptGetMS    float64
	detectionMS    float64
	processingMS   float64
	jobDurationMS  float64
	initial200     int
	newer200       int
	unchanged200   int
	noContent204   int
	errors         int
	cancellations  int
	totalRequests  int
	// dbReads counts observation requests that reached the server and read
	// authoritative state (every 200 and every deadline recheck that became a
	// 204). The held wait itself never polls the database.
	dbReads            int
	baselineVersion    int
	terminalVersion    int
	distinctNewSeen    int
	versionCoverage    float64
	variantsCompleted  int
	status             string
	serverCompletedAt  string
	clientCompletedAt  string
	observationStartAt string
	notes              string
}

func main() {
	var cfg config
	flag.StringVar(&cfg.base, "base", "http://localhost:4000", "API base URL")
	flag.StringVar(&cfg.image, "image", "", "Path to the test image to upload")
	flag.StringVar(&cfg.condition, "condition", "B", "Condition id (A, B, C1, C2, C3)")
	flag.StringVar(&cfg.label, "label", "", "Human label for the condition")
	flag.StringVar(&cfg.mode, "mode", "short", "Observation mode: short or long")
	flag.IntVar(&cfg.reps, "reps", 3, "Repetitions for this condition/mode")
	flag.IntVar(&cfg.latencyMS, "latency", 0, "Additional round-trip latency in ms (split before/after each observation request)")
	flag.IntVar(&cfg.quietDelayMS, "quiet-delay", 0, "Recorded quiet delay (ms), for the CSV")
	flag.IntVar(&cfg.simulateDurMS, "simulate-duration", 0, "Recorded simulated phase duration (ms), for the CSV")
	flag.IntVar(&cfg.simulateIntMS, "simulate-interval", 0, "Recorded simulated update interval (ms), for the CSV")
	flag.StringVar(&cfg.serverLog, "server-log", "", "Server log file to parse terminal commit times from")
	flag.StringVar(&cfg.csvPath, "out", "experiments/results/runs.csv", "CSV file to append rows to")
	flag.StringVar(&cfg.rawDir, "rawdir", "experiments/results/raw", "Directory for per-run JSONL raw logs")
	flag.IntVar(&cfg.shortIntervalMS, "short-interval", 1000, "Short polling interval (ms)")
	flag.IntVar(&cfg.longWaitSeconds, "long-wait", 20, "Long polling wait parameter (seconds)")
	flag.IntVar(&cfg.requestTimeoutMS, "request-timeout", 30000, "Per-request client timeout (ms)")
	flag.Parse()

	if cfg.image == "" {
		fmt.Fprintln(os.Stderr, "error: -image is required")
		os.Exit(2)
	}
	if cfg.mode != "short" && cfg.mode != "long" {
		fmt.Fprintln(os.Stderr, "error: -mode must be short or long")
		os.Exit(2)
	}
	if err := os.MkdirAll(cfg.rawDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "error: cannot create raw dir:", err)
		os.Exit(1)
	}

	client := &http.Client{Timeout: time.Duration(cfg.requestTimeoutMS) * time.Millisecond}

	for rep := 1; rep <= cfg.reps; rep++ {
		res, err := runOnce(client, cfg, rep)
		if err != nil {
			fmt.Fprintf(os.Stderr, "run %s/%s rep %d failed: %v\n", cfg.condition, cfg.mode, rep, err)
			continue
		}
		if err := appendCSV(cfg.csvPath, res); err != nil {
			fmt.Fprintln(os.Stderr, "error writing CSV:", err)
			os.Exit(1)
		}
		fmt.Printf("run %-12s mode=%-5s rep=%d detection=%7.1fms requests=%d coverage=%.2f\n",
			res.runID, res.mode, rep, res.detectionMS, res.totalRequests, res.versionCoverage)
	}
}

// runOnce uploads one image and observes it to a terminal state, returning the
// metrics for one run and writing the per-run raw JSONL log.
func runOnce(client *http.Client, cfg config, rep int) (*result, error) {
	runID := fmt.Sprintf("%s-%s-%d-%s", cfg.condition, cfg.mode, rep, time.Now().Format("150405.000"))

	rawPath := filepath.Join(cfg.rawDir, runID+".jsonl")
	raw, err := os.Create(rawPath)
	if err != nil {
		return nil, err
	}
	defer raw.Close()

	logEvent := func(ev event) {
		ev.Run = runID
		ev.At = time.Now().UTC().Format(time.RFC3339Nano)
		b, _ := json.Marshal(ev)
		raw.Write(b)
		raw.Write([]byte("\n"))
	}

	// --- upload ---
	uploadStart := time.Now()
	accepted, err := upload(client, cfg.base, cfg.image)
	ackMS := float64(time.Since(uploadStart).Microseconds()) / 1000.0
	if err != nil {
		logEvent(event{Event: "upload_error", Note: err.Error()})
		return nil, err
	}
	logEvent(event{Event: "ack", HTTP: 202, Version: accepted.Version, Note: accepted.JobID})

	// latencySetting documents the actual added-latency configuration
	// verbatim (the brief asks to report the actual setting if tooling cannot
	// produce exactly 200 ms).
	latencySetting := "none"
	if cfg.latencyMS > 0 {
		latencySetting = fmt.Sprintf("%dms added round-trip (%dms before + %dms after each observation request)",
			cfg.latencyMS, cfg.latencyMS/2, cfg.latencyMS/2)
	}

	res := &result{
		runID:              runID,
		jobID:              accepted.JobID,
		image:              filepath.Base(cfg.image),
		condition:          cfg.condition,
		label:              cfg.label,
		mode:               cfg.mode,
		rep:                rep,
		latencySetting:     latencySetting,
		quietDelayMS:       cfg.quietDelayMS,
		simulateDurMS:      cfg.simulateDurMS,
		simulateIntMS:      cfg.simulateIntMS,
		latencyMS:          cfg.latencyMS,
		uploadAckMS:        ackMS,
		status:             accepted.Status,
		observationStartAt: time.Now().UTC().Format(time.RFC3339Nano),
		notes:              runNotes(cfg),
	}

	statusURL := cfg.base + accepted.StatusURL
	latencyHalf := time.Duration(cfg.latencyMS/2) * time.Millisecond

	// seen records every version number actually received from an observation
	// GET. The baseline is the first observed version, so versions that only
	// existed in the 202 response are not counted.
	seen := map[int]bool{}
	versionFirstSeen := -1
	versionLastSeen := accepted.Version
	var terminal snapshot

	// observe performs one GET and classifies the response.
	observe := func(seq int, url string) (snap *snapshot, httpStatus int, isTerminal bool, err error) {
		if latencyHalf > 0 {
			time.Sleep(latencyHalf)
		}
		logEvent(event{Event: "request", Seq: seq, Mode: cfg.mode, Note: url})
		rtStart := time.Now()
		resp, err := client.Get(url)
		if err != nil {
			logEvent(event{Event: "error", Seq: seq, Note: err.Error()})
			return nil, 0, false, err
		}
		defer resp.Body.Close()
		res.totalRequests++

		if resp.StatusCode == http.StatusNoContent {
			res.noContent204++
			res.dbReads++
			if latencyHalf > 0 {
				time.Sleep(latencyHalf)
			}
			logEvent(event{Event: "response", Seq: seq, HTTP: 204, Mode: cfg.mode})
			return nil, 204, false, nil
		}
		if resp.StatusCode != http.StatusOK {
			res.errors++
			if latencyHalf > 0 {
				time.Sleep(latencyHalf)
			}
			logEvent(event{Event: "response", Seq: seq, HTTP: resp.StatusCode, Mode: cfg.mode})
			return nil, resp.StatusCode, false, fmt.Errorf("HTTP %d", resp.StatusCode)
		}

		var s snapshot
		if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
			res.errors++
			logEvent(event{Event: "error", Seq: seq, Note: err.Error()})
			return nil, 200, false, err
		}
		res.dbReads++
		if seq == 1 {
			// Prompt-GET timing check: the real HTTP round trip, excluding the
			// documented injected latency sleeps.
			res.promptGetMS = float64(time.Since(rtStart).Microseconds()) / 1000.0
		}
		if latencyHalf > 0 {
			time.Sleep(latencyHalf)
		}

		if versionFirstSeen == -1 {
			versionFirstSeen = s.Version
		}
		if !seen[s.Version] {
			seen[s.Version] = true
		}
		logEvent(event{Event: "response", Seq: seq, HTTP: 200, Version: s.Version, Status: s.Status, Mode: cfg.mode})

		term := s.Status == "completed" || s.Status == "failed"
		return &s, 200, term, nil
	}

	// Initial prompt snapshot (API-01), just like the frontend.
	seq := 1
	snap, st, term, err := observe(seq, statusURL)
	if err != nil {
		// A transport error on the very first read ends the run.
		if st == 0 {
			return nil, err
		}
	}
	if st == 200 {
		res.initial200++
		if snap != nil {
			versionLastSeen = snap.Version
			applySnapshot(res, snap)
			terminal = *snap
		}
	}

	if term {
		finalize(res, cfg, terminal, seen, versionFirstSeen, logEvent)
		return res, nil
	}

	// --- observation loop ---
	for {
		seq++
		var url string
		if cfg.mode == "long" {
			url = fmt.Sprintf("%s?after=%d&wait=%d", statusURL, versionLastSeen, cfg.longWaitSeconds)
		} else {
			time.Sleep(time.Duration(cfg.shortIntervalMS) * time.Millisecond)
			url = statusURL
		}

		s, status, term, err := observe(seq, url)
		if err != nil {
			if status == 0 {
				// Transport failure or timeout: stop, but keep what we have.
				res.errors++
				break
			}
			continue
		}
		if status == 204 {
			continue
		}
		if s == nil {
			continue
		}

		// Count 200s as newer or unchanged relative to the previously seen
		// version (the initial 200 is counted separately).
		if seq > 1 {
			if s.Version > versionLastSeen {
				res.newer200++
			} else {
				res.unchanged200++
			}
		}
		versionLastSeen = s.Version
		applySnapshot(res, s)
		terminal = *s
		if term {
			break
		}
	}

	finalize(res, cfg, terminal, seen, versionFirstSeen, logEvent)
	return res, nil
}

// applySnapshot copies progress fields from a snapshot onto the result.
func applySnapshot(res *result, s *snapshot) {
	res.status = s.Status
	res.variantsCompleted = s.VariantsCompleted
}

// finalize fills the derived metrics, joins the server commit time, and logs
// the terminal event.
func finalize(res *result, cfg config, terminal snapshot, seen map[int]bool, versionFirstSeen int, logEvent func(event)) {
	if versionFirstSeen == -1 {
		versionFirstSeen = terminal.Version
	}
	res.baselineVersion = versionFirstSeen
	res.terminalVersion = terminal.Version

	// Distinct versions received, and the "new" versions beyond the baseline.
	distinct := 0
	for v := 1; v <= terminal.Version; v++ {
		if seen[v] {
			distinct++
		}
	}
	if distinct > 0 {
		res.distinctNewSeen = distinct - 1
	}
	// Version coverage compares the new versions received with the versions
	// committed after the baseline through the terminal version, so gaps lower
	// the ratio (Section 6).
	if span := terminal.Version - res.baselineVersion; span > 0 {
		res.versionCoverage = float64(res.distinctNewSeen) / float64(span)
	} else {
		res.versionCoverage = 1
	}

	res.clientCompletedAt = logEventReturningNow(logEvent, res, terminal)

	// processing duration and job duration come from server timestamps.
	if terminal.StartedAt != nil {
		if start, err := time.Parse(time.RFC3339Nano, *terminal.StartedAt); err == nil {
			if terminal.CompletedAt != nil {
				if end, err := time.Parse(time.RFC3339Nano, *terminal.CompletedAt); err == nil {
					res.processingMS = float64(end.Sub(start).Microseconds()) / 1000.0
				}
			}
		}
	}
	if terminal.CompletedAt != nil {
		if end, err := time.Parse(time.RFC3339Nano, *terminal.CompletedAt); err == nil {
			if q, err := time.Parse(time.RFC3339Nano, terminal.QueuedAt); err == nil {
				res.jobDurationMS = float64(end.Sub(q).Microseconds()) / 1000.0
			}
		}
		res.serverCompletedAt = *terminal.CompletedAt
	}

	// Detection delay: client receipt minus the server's post-commit log time.
	if cfg.serverLog != "" {
		if commit, ok := terminalCommitTime(cfg.serverLog, res.jobID, terminal.Status); ok {
			receipt, _ := time.Parse(time.RFC3339Nano, res.clientCompletedAt)
			res.detectionMS = float64(receipt.Sub(commit).Microseconds()) / 1000.0
		}
	}
}

// logEventReturningNow logs a terminal event and returns the receipt time.
func logEventReturningNow(logEvent func(event), res *result, terminal snapshot) string {
	now := time.Now().UTC()
	logEvent(event{Event: "terminal", HTTP: 200, Version: terminal.Version, Status: terminal.Status})
	return now.Format(time.RFC3339Nano)
}

// upload posts the image and decodes the 202 acceptance body.
func upload(client *http.Client, base, imagePath string) (*accept, error) {
	f, err := os.Open(imagePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", filepath.Base(imagePath))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, f); err != nil {
		return nil, err
	}
	w.Close()

	req, err := http.NewRequest(http.MethodPost, base+"/v1/images", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upload returned HTTP %d: %s", resp.StatusCode, string(b))
	}
	var a accept
	if err := json.NewDecoder(resp.Body).Decode(&a); err != nil {
		return nil, err
	}
	return &a, nil
}

var commitRe = regexp.MustCompile(`status=(\w+).*committed_at=(\S+)`)

// terminalCommitTime scans the server log for the post-commit wall-clock time
// of a specific job's terminal commit. Joining by job id keeps runs that share
// one server process (both modes, every repetition) apart. The log line is:
//
//	msg="job change committed" job_id=... status=completed version=10 committed_at=2026-...Z
func terminalCommitTime(logPath, jobID, terminalStatus string) (time.Time, bool) {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return time.Time{}, false
	}
	var latest time.Time
	found := false
	for _, line := range bytes.Split(data, []byte("\n")) {
		if !bytes.Contains(line, []byte("job change committed")) {
			continue
		}
		if !bytes.Contains(line, []byte(jobID)) {
			continue
		}
		m := commitRe.FindSubmatch(line)
		if m == nil || string(m[1]) != terminalStatus {
			continue
		}
		if ts, err := time.Parse(time.RFC3339Nano, string(m[2])); err == nil {
			if !found || ts.After(latest) {
				latest = ts
				found = true
			}
		}
	}
	return latest, found
}

// runNotes records the timing semantics and applied workload controls for a
// run, so the methods section can state exactly how each mode was driven.
func runNotes(cfg config) string {
	var parts []string
	if cfg.mode == "short" {
		parts = append(parts, fmt.Sprintf("short: %dms delay after each response, no overlapping requests", cfg.shortIntervalMS))
	} else {
		parts = append(parts, fmt.Sprintf("long: next request immediately after each active 200 or 204, wait=%ds", cfg.longWaitSeconds))
	}
	if cfg.quietDelayMS > 0 {
		parts = append(parts, fmt.Sprintf("server quiet-delay=%dms (no intermediate updates)", cfg.quietDelayMS))
	}
	if cfg.simulateDurMS > 0 {
		parts = append(parts, fmt.Sprintf("server simulated phase=%dms at %dms updates (labelled simulated, not real progress)", cfg.simulateDurMS, cfg.simulateIntMS))
	}
	if cfg.latencyMS > 0 {
		parts = append(parts, fmt.Sprintf("client added %dms round-trip", cfg.latencyMS))
	}
	parts = append(parts, "detection = client receipt - server post-commit log time, joined by job id")
	return strings.Join(parts, "; ")
}

// appendCSV appends one result row. The first columns are the exact set the
// brief requires; the trailing columns add supporting detail.
func appendCSV(path string, res *result) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	needHeader := true
	if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
		needHeader = false
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{
		"run_id", "condition", "mode", "repetition", "job_id", "image", "latency_setting",
		"baseline_version", "terminal_version", "requests_started",
		"initial_200", "newer_200", "unchanged_200", "no_change_204",
		"retrieval_errors", "cancellations", "distinct_new_versions_seen",
		"processing_ms", "detection_ms", "notes",
		"observation_db_reads", "version_coverage", "job_duration_ms", "upload_ack_ms",
		"prompt_get_ms", "variants_completed", "status",
		"condition_label", "quiet_delay_ms", "simulate_duration_ms", "simulate_interval_ms",
		"client_latency_ms", "observation_started_at", "server_completed_at", "client_completed_at",
	}
	if needHeader {
		if err := w.Write(header); err != nil {
			return err
		}
	}

	row := []string{
		res.runID, res.condition, res.mode, fmt.Sprintf("%d", res.rep), res.jobID, res.image, res.latencySetting,
		fmt.Sprintf("%d", res.baselineVersion), fmt.Sprintf("%d", res.terminalVersion), fmt.Sprintf("%d", res.totalRequests),
		fmt.Sprintf("%d", res.initial200), fmt.Sprintf("%d", res.newer200), fmt.Sprintf("%d", res.unchanged200),
		fmt.Sprintf("%d", res.noContent204), fmt.Sprintf("%d", res.errors), fmt.Sprintf("%d", res.cancellations),
		fmt.Sprintf("%d", res.distinctNewSeen), fmt.Sprintf("%.1f", res.processingMS), fmt.Sprintf("%.1f", res.detectionMS), res.notes,
		fmt.Sprintf("%d", res.dbReads), fmt.Sprintf("%.4f", res.versionCoverage), fmt.Sprintf("%.1f", res.jobDurationMS),
		fmt.Sprintf("%.1f", res.uploadAckMS), fmt.Sprintf("%.2f", res.promptGetMS), fmt.Sprintf("%d", res.variantsCompleted), res.status,
		res.label, fmt.Sprintf("%d", res.quietDelayMS), fmt.Sprintf("%d", res.simulateDurMS), fmt.Sprintf("%d", res.simulateIntMS),
		fmt.Sprintf("%d", res.latencyMS), res.observationStartAt, res.serverCompletedAt, res.clientCompletedAt,
	}
	return w.Write(row)
}

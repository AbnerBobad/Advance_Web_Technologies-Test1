// Command experiment-summary reads experiments/results/runs.csv and produces
// a per-condition summary (median / min / max) plus two labelled SVG charts:
// completion-detection delay and status requests per job, each grouped by
// condition and observation mode.
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var conditionOrder = []string{"A", "B", "C1", "C2", "C3"}
var modeOrder = []string{"short", "long"}
var modeColor = map[string]string{"short": "#4f8fc0", "long": "#e0975a"}

type row struct {
	condition    string
	label        string
	mode         string
	detectionMS  float64
	requests     int
	coverage     float64
	processingMS float64
}

type stats struct {
	condition     string
	label         string
	mode          string
	n             int
	detMed        float64
	detMin        float64
	detMax        float64
	reqMed        float64
	reqMin        float64
	reqMax        float64
	coverageMed   float64
	processingMed float64
}

func main() {
	var csvPath, outDir string
	flag.StringVar(&csvPath, "csv", "experiments/results/runs.csv", "runs CSV")
	flag.StringVar(&outDir, "outdir", "experiments/results", "output directory")
	flag.Parse()

	rows, err := readRows(csvPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	summaries := summarise(rows)

	if err := writeSummary(filepath.Join(outDir, "summary.csv"), summaries); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	labels := map[string]string{}
	for _, s := range summaries {
		labels[s.condition] = s.label
	}

	if err := os.WriteFile(filepath.Join(outDir, "chart-detection.svg"),
		[]byte(chart("Completion-detection delay by condition and mode (median)",
			"milliseconds",
			summaries, func(s stats) float64 { return s.detMed }, formatMS)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	if err := os.WriteFile(filepath.Join(outDir, "chart-requests.svg"),
		[]byte(chart("Status requests per job by condition and mode (median)",
			"requests",
			summaries, func(s stats) float64 { return s.reqMed }, formatInt)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	fmt.Println("wrote summary.csv and 2 charts to", outDir)
}

func readRows(path string) ([]row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("no data rows in %s", path)
	}
	idx := map[string]int{}
	for i, name := range records[0] {
		idx[name] = i
	}
	get := func(rec []string, key string) string {
		i, ok := idx[key]
		if !ok || i >= len(rec) {
			return ""
		}
		return rec[i]
	}

	out := make([]row, 0, len(records)-1)
	for _, rec := range records[1:] {
		out = append(out, row{
			condition:    get(rec, "condition"),
			label:        get(rec, "condition_label"),
			mode:         get(rec, "mode"),
			detectionMS:  atof(get(rec, "detection_ms")),
			requests:     atoi(get(rec, "requests_started")),
			coverage:     atof(get(rec, "version_coverage")),
			processingMS: atof(get(rec, "processing_ms")),
		})
	}
	return out, nil
}

// summarise groups rows by (condition, mode) and computes median/min/max.
func summarise(rows []row) []stats {
	type key struct{ cond, mode string }
	grouped := map[key][]row{}
	for _, r := range rows {
		grouped[key{r.condition, r.mode}] = append(grouped[key{r.condition, r.mode}], r)
	}

	var out []stats
	for _, cond := range conditionOrder {
		for _, mode := range modeOrder {
			g := grouped[key{cond, mode}]
			if len(g) == 0 {
				continue
			}
			var det, req, cov, proc []float64
			label := cond
			for _, r := range g {
				det = append(det, r.detectionMS)
				req = append(req, float64(r.requests))
				cov = append(cov, r.coverage)
				proc = append(proc, r.processingMS)
				if r.label != "" {
					label = r.label
				}
			}
			out = append(out, stats{
				condition:     cond,
				label:         label,
				mode:          mode,
				n:             len(g),
				detMed:        median(det),
				detMin:        minOf(det),
				detMax:        maxOf(det),
				reqMed:        median(req),
				reqMin:        minOf(req),
				reqMax:        maxOf(req),
				coverageMed:   median(cov),
				processingMed: median(proc),
			})
		}
	}
	return out
}

func writeSummary(path string, summaries []stats) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{
		"condition", "condition_label", "mode", "n",
		"detection_median_ms", "detection_min_ms", "detection_max_ms",
		"requests_median", "requests_min", "requests_max",
		"coverage_median", "processing_median_ms",
	}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, s := range summaries {
		rec := []string{
			s.condition, s.label, s.mode, strconv.Itoa(s.n),
			fmt.Sprintf("%.1f", s.detMed), fmt.Sprintf("%.1f", s.detMin), fmt.Sprintf("%.1f", s.detMax),
			fmt.Sprintf("%.1f", s.reqMed), fmt.Sprintf("%.0f", s.reqMin), fmt.Sprintf("%.0f", s.reqMax),
			fmt.Sprintf("%.4f", s.coverageMed), fmt.Sprintf("%.1f", s.processingMed),
		}
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	return nil
}

// ---------- SVG bar chart ----------

func formatMS(v float64) string {
	if v >= 1000 {
		return fmt.Sprintf("%.2fs", v/1000)
	}
	return fmt.Sprintf("%.0fms", v)
}

func formatInt(v float64) string { return fmt.Sprintf("%.0f", v) }

func chart(title, yLabel string, summaries []stats, value func(stats) float64, format func(float64) string) string {
	const (
		w, h   = 760.0, 440.0
		left   = 80.0
		right  = 20.0
		top    = 60.0
		bottom = 90.0
		groupW = (w - left - right)
	)

	// index values by condition+mode
	val := map[string]float64{}
	for _, s := range summaries {
		val[s.condition+"|"+s.mode] = value(s)
	}

	maxV := 0.0
	for _, v := range val {
		if v > maxV {
			maxV = v
		}
	}
	if maxV <= 0 {
		maxV = 1
	}
	axisMax := niceCeil(maxV)

	plotW := w - left - right
	plotH := h - top - bottom
	nGroups := len(conditionOrder)
	slot := plotW / float64(nGroups)
	barW := slot * 0.28
	y := func(v float64) float64 { return top + plotH - (v/axisMax)*plotH }

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" viewBox="0 0 %.0f %.0f" font-family="system-ui, sans-serif">`, w, h, w, h)
	b.WriteString(`<rect width="100%" height="100%" fill="#ffffff"/>`)
	fmt.Fprintf(&b, `<text x="%.0f" y="30" font-size="17" font-weight="600" fill="#222">%s</text>`, left-10, esc(title))

	// y grid + ticks
	const ticks = 5
	for i := 0; i <= ticks; i++ {
		v := axisMax * float64(i) / ticks
		yy := y(v)
		fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#e3e3e3" stroke-width="1"/>`, left, yy, w-right, yy)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-size="11" fill="#666" text-anchor="end">%s</text>`, left-8, yy+4, format(v))
	}

	// axis labels
	fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-size="12" fill="#444" text-anchor="middle">%s</text>`, (left+w-right)/2, h-14, esc(yLabel))
	fmt.Fprintf(&b, `<text x="18" y="%.1f" font-size="12" fill="#444" text-anchor="middle" transform="rotate(-90 18 %.1f)">%s</text>`, top+plotH/2, top+plotH/2, esc(yLabel))

	// bars + x labels
	for gi, cond := range conditionOrder {
		groupCenter := left + slot*float64(gi) + slot/2
		for mi, mode := range modeOrder {
			v, ok := val[cond+"|"+mode]
			if !ok {
				continue
			}
			bx := groupCenter - barW - 4 + float64(mi)*(barW+8)
			by := y(v)
			bh := top + plotH - by
			fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s" rx="2"/>`,
				bx, by, barW, bh, modeColor[mode])
			fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-size="10" fill="#333" text-anchor="middle">%s</text>`,
				bx+barW/2, by-4, format(v))
		}
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-size="13" font-weight="600" fill="#222" text-anchor="middle">%s</text>`,
			groupCenter, top+plotH+20, esc(cond))
	}

	// legend
	lx := left
	ly := h - 46.0
	for _, mode := range modeOrder {
		fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="14" height="14" fill="%s" rx="2"/>`, lx, ly-11, modeColor[mode])
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-size="12" fill="#333">%s polling</text>`, lx+20, ly, mode)
		lx += 130
	}
	b.WriteString(`</svg>`)
	return b.String()
}

func niceCeil(v float64) float64 {
	if v <= 0 {
		return 1
	}
	mag := math.Pow(10, math.Floor(math.Log10(v)))
	n := v / mag
	switch {
	case n <= 1:
		n = 1
	case n <= 2:
		n = 2
	case n <= 5:
		n = 5
	default:
		n = 10
	}
	return n * mag
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	m := len(s) / 2
	if len(s)%2 == 1 {
		return s[m]
	}
	return (s[m-1] + s[m]) / 2
}

func minOf(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	m := xs[0]
	for _, x := range xs {
		if x < m {
			m = x
		}
	}
	return m
}

func maxOf(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	m := xs[0]
	for _, x := range xs {
		if x > m {
			m = x
		}
	}
	return m
}

func atof(s string) float64 { v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64); return v }
func atoi(s string) int     { v, _ := strconv.Atoi(strings.TrimSpace(s)); return v }
func esc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

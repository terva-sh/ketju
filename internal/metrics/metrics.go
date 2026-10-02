// Package metrics is a small Prometheus text exposition: counters, gauges,
// histograms with fixed buckets, and callbacks read at scrape time
// (TKT-01M29CEE7S, docs/02 "Observability").
//
// It is written by hand rather than taken from prometheus/client_golang,
// which would add about ten modules to a binary that needs three metric
// types. Everything a request touches is an atomic, and a scrape takes no
// lock a request takes: a labelled series is found through a copy-on-write
// map that a reader loads without locking, and only the first observation
// of a new label set takes the family's mutex, which no scrape takes.
//
// Registering a name or label that breaks the rules panics, so a bad name
// stops the server at startup rather than reaching a scraper.
package metrics

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Type is a metric family's type, as the exposition's # TYPE line names it.
type Type string

const (
	TypeCounter   Type = "counter"
	TypeGauge     Type = "gauge"
	TypeHistogram Type = "histogram"
)

// Name and label rules. Every name is ketju_ and lowercase, so a scrape of
// this process cannot collide with another exporter's series.
var (
	nameRule  = regexp.MustCompile(`^ketju_[a-z][a-z0-9_]*$`)
	labelRule = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// CheckName reports whether name is a legal metric name for a family of
// type t: ketju_ and lowercase, ending in _total exactly when it is a
// counter, and with no suffix a histogram's series add.
func CheckName(name string, t Type) error {
	if !nameRule.MatchString(name) {
		return fmt.Errorf("metrics: %q does not match %s", name, nameRule)
	}
	isTotal := strings.HasSuffix(name, "_total")
	switch {
	case t == TypeCounter && !isTotal:
		return fmt.Errorf("metrics: counter %q must end in _total", name)
	case t != TypeCounter && isTotal:
		return fmt.Errorf("metrics: %s %q must not end in _total", t, name)
	}
	for _, s := range []string{"_bucket", "_sum", "_count"} {
		if strings.HasSuffix(name, s) {
			return fmt.Errorf("metrics: %q ends in %s, which a histogram's series use", name, s)
		}
	}
	return nil
}

func checkLabels(name string, labels []string) error {
	seen := map[string]bool{}
	for _, l := range labels {
		if !labelRule.MatchString(l) || l == "le" {
			return fmt.Errorf("metrics: %s: label %q is not allowed", name, l)
		}
		if seen[l] {
			return fmt.Errorf("metrics: %s: label %q is repeated", name, l)
		}
		seen[l] = true
	}
	return nil
}

// Registry holds the families a scrape writes.
type Registry struct {
	// mu guards fams, which only registration writes. A scrape copies the
	// slice under it before writing anything, so a slow client holds it for
	// no longer than the copy. No request takes it.
	mu   sync.Mutex
	fams map[string]*family
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{fams: map[string]*family{}} }

// family is one name: its help, type, label names, and how to read it.
type family struct {
	name, help string
	typ        Type
	labels     []string
	// One of these says how to read it.
	collect   func(emit func(Sample))
	histogram *HistogramVec
}

// Sample is one series of a counter or a gauge: its label values, in the
// order the family declared its labels, and its value.
type Sample struct {
	Labels []string
	Value  float64
}

func (r *Registry) register(f *family) {
	if err := CheckName(f.name, f.typ); err != nil {
		panic(err)
	}
	if err := checkLabels(f.name, f.labels); err != nil {
		panic(err)
	}
	if (f.collect == nil) == (f.histogram == nil) {
		panic(fmt.Sprintf("metrics: %s needs exactly one way to be read", f.name))
	}
	if strings.TrimSpace(f.help) == "" {
		panic(fmt.Sprintf("metrics: %s has no help text", f.name))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.fams[f.name]; ok {
		panic(fmt.Sprintf("metrics: %s is registered twice", f.name))
	}
	r.fams[f.name] = f
}

// Names is every registered name, sorted.
func (r *Registry) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.fams))
	for n := range r.fams {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// TypeOf is the type a name was registered with, and false for a name that
// was not.
func (r *Registry) TypeOf(name string) (Type, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.fams[name]
	if !ok {
		return "", false
	}
	return f.typ, true
}

// LabelsOf is the label names a name was registered with.
func (r *Registry) LabelsOf(name string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if f, ok := r.fams[name]; ok {
		return slices.Clone(f.labels)
	}
	return nil
}

// Counter is a monotonic count.
type Counter struct{ v atomic.Uint64 }

// Inc adds one.
func (c *Counter) Inc() { c.v.Add(1) }

// Add adds n.
func (c *Counter) Add(n uint64) { c.v.Add(n) }

// Value is the count.
func (c *Counter) Value() uint64 { return c.v.Load() }

// Counter registers and returns an unlabelled counter.
func (r *Registry) Counter(name, help string) *Counter {
	c := &Counter{}
	r.register(&family{name: name, help: help, typ: TypeCounter, collect: func(emit func(Sample)) {
		emit(Sample{Value: float64(c.Value())})
	}})
	return c
}

// Gauge is a value that goes up and down.
type Gauge struct{ bits atomic.Uint64 }

// Set sets the value.
func (g *Gauge) Set(v float64) { g.bits.Store(math.Float64bits(v)) }

// Value is the value.
func (g *Gauge) Value() float64 { return math.Float64frombits(g.bits.Load()) }

// Gauge registers and returns an unlabelled gauge.
func (r *Registry) Gauge(name, help string) *Gauge {
	g := &Gauge{}
	r.register(&family{name: name, help: help, typ: TypeGauge, collect: func(emit func(Sample)) {
		emit(Sample{Value: g.Value()})
	}})
	return g
}

// CounterFunc registers a counter family read at scrape time. collect
// emits one Sample per series, with a value for each of labels. It must
// take no lock a request takes.
func (r *Registry) CounterFunc(name, help string, labels []string, collect func(emit func(Sample))) {
	r.register(&family{name: name, help: help, typ: TypeCounter, labels: slices.Clone(labels), collect: collect})
}

// GaugeFunc registers a gauge family read at scrape time, as CounterFunc.
func (r *Registry) GaugeFunc(name, help string, labels []string, collect func(emit func(Sample))) {
	r.register(&family{name: name, help: help, typ: TypeGauge, labels: slices.Clone(labels), collect: collect})
}

// Histogram counts observations into fixed buckets. Each bucket is its own
// atomic, and the sum is a float updated by compare and swap, so an
// observation takes no lock.
type Histogram struct {
	bounds []float64       // upper bounds, ascending, without +Inf
	counts []atomic.Uint64 // one per bound, then +Inf; not cumulative
	sum    atomic.Uint64   // float64 bits
}

func newHistogram(bounds []float64) *Histogram {
	return &Histogram{bounds: bounds, counts: make([]atomic.Uint64, len(bounds)+1)}
}

// Observe records one value.
func (h *Histogram) Observe(v float64) {
	i, _ := slices.BinarySearch(h.bounds, v)
	h.counts[i].Add(1)
	for {
		old := h.sum.Load()
		if h.sum.CompareAndSwap(old, math.Float64bits(math.Float64frombits(old)+v)) {
			return
		}
	}
}

// snapshot reads the cumulative bucket counts, the count and the sum. The
// count is the +Inf bucket, so the two always agree in one scrape; the sum
// may be one observation ahead, which the exposition format allows.
func (h *Histogram) snapshot() (cum []uint64, count uint64, sum float64) {
	cum = make([]uint64, len(h.counts))
	var acc uint64
	for i := range h.counts {
		acc += h.counts[i].Load()
		cum[i] = acc
	}
	return cum, acc, math.Float64frombits(h.sum.Load())
}

func checkBounds(name string, bounds []float64) []float64 {
	if len(bounds) == 0 {
		panic(fmt.Sprintf("metrics: histogram %s has no buckets", name))
	}
	for i, b := range bounds {
		if math.IsNaN(b) || math.IsInf(b, 0) || (i > 0 && b <= bounds[i-1]) {
			panic(fmt.Sprintf("metrics: histogram %s: buckets must be finite and ascending", name))
		}
	}
	return slices.Clone(bounds)
}

// HistogramVec is a histogram family with labels. Each label set is its own
// series, made on its first observation.
type HistogramVec struct {
	name   string
	labels []string
	bounds []float64

	// series is read without a lock, by requests and scrapes alike. Only
	// adding a label set takes mu, copies the map, and stores the copy.
	series atomic.Pointer[map[string]*labelled]
	mu     sync.Mutex
}

type labelled struct {
	values []string
	h      *Histogram
}

// HistogramVec registers and returns a labelled histogram with the given
// upper bounds.
func (r *Registry) HistogramVec(name, help string, bounds []float64, labels ...string) *HistogramVec {
	v := &HistogramVec{name: name, labels: slices.Clone(labels), bounds: checkBounds(name, bounds)}
	empty := map[string]*labelled{}
	v.series.Store(&empty)
	r.register(&family{name: name, help: help, typ: TypeHistogram, labels: v.labels, histogram: v})
	return v
}

// With returns the series for values, which must be one per label.
func (v *HistogramVec) With(values ...string) *Histogram {
	if len(values) != len(v.labels) {
		panic(fmt.Sprintf("metrics: %s takes %d label values, got %d", v.name, len(v.labels), len(values)))
	}
	key := strings.Join(values, "\xff")
	if s, ok := (*v.series.Load())[key]; ok {
		return s.h
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	cur := *v.series.Load()
	if s, ok := cur[key]; ok {
		return s.h
	}
	next := make(map[string]*labelled, len(cur)+1)
	for k, s := range cur {
		next[k] = s
	}
	s := &labelled{values: slices.Clone(values), h: newHistogram(v.bounds)}
	next[key] = s
	v.series.Store(&next)
	return s.h
}

// Series is how many label sets have been observed.
func (v *HistogramVec) Series() int { return len(*v.series.Load()) }

// ContentType is the text exposition format's media type.
const ContentType = "text/plain; version=0.0.4; charset=utf-8"

// WriteText writes every family in the text exposition format 0.0.4:
// families sorted by name, series sorted by label values.
func (r *Registry) WriteText(w io.Writer) error {
	r.mu.Lock()
	fams := make([]*family, 0, len(r.fams))
	for _, f := range r.fams {
		fams = append(fams, f)
	}
	r.mu.Unlock()
	slices.SortFunc(fams, func(a, b *family) int { return strings.Compare(a.name, b.name) })

	var b strings.Builder
	for _, f := range fams {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n", f.name, escapeHelp(f.help), f.name, f.typ)
		if f.histogram != nil {
			writeHistogram(&b, f)
			continue
		}
		var samples []Sample
		f.collect(func(s Sample) {
			if len(s.Labels) != len(f.labels) {
				panic(fmt.Sprintf("metrics: %s emitted %d label values for %d labels", f.name, len(s.Labels), len(f.labels)))
			}
			samples = append(samples, Sample{Labels: slices.Clone(s.Labels), Value: s.Value})
		})
		slices.SortFunc(samples, func(a, b Sample) int { return slices.Compare(a.Labels, b.Labels) })
		for _, s := range samples {
			b.WriteString(f.name)
			writeLabels(&b, f.labels, s.Labels, "")
			b.WriteByte(' ')
			b.WriteString(formatValue(s.Value))
			b.WriteByte('\n')
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writeHistogram(b *strings.Builder, f *family) {
	v := f.histogram
	all := *v.series.Load()
	list := make([]*labelled, 0, len(all))
	for _, s := range all {
		list = append(list, s)
	}
	slices.SortFunc(list, func(a, b *labelled) int { return slices.Compare(a.values, b.values) })
	for _, s := range list {
		cum, count, sum := s.h.snapshot()
		for i, c := range cum {
			le := "+Inf"
			if i < len(v.bounds) {
				le = formatValue(v.bounds[i])
			}
			b.WriteString(f.name + "_bucket")
			writeLabels(b, f.labels, s.values, le)
			fmt.Fprintf(b, " %d\n", c)
		}
		b.WriteString(f.name + "_sum")
		writeLabels(b, f.labels, s.values, "")
		b.WriteString(" " + formatValue(sum) + "\n")
		b.WriteString(f.name + "_count")
		writeLabels(b, f.labels, s.values, "")
		fmt.Fprintf(b, " %d\n", count)
	}
}

func writeLabels(b *strings.Builder, names, values []string, le string) {
	if len(names) == 0 && le == "" {
		return
	}
	b.WriteByte('{')
	for i, n := range names {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(n + `="` + escapeLabel(values[i]) + `"`)
	}
	if le != "" {
		if len(names) > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`le="` + le + `"`)
	}
	b.WriteByte('}')
}

func formatValue(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	case math.IsNaN(v):
		return "NaN"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

var (
	helpEscaper  = strings.NewReplacer(`\`, `\\`, "\n", `\n`)
	labelEscaper = strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`)
)

func escapeHelp(s string) string  { return helpEscaper.Replace(s) }
func escapeLabel(s string) string { return labelEscaper.Replace(s) }

// Handler serves the registry's text exposition at exactly /metrics, for GET
// and HEAD, and answers 404 for every other path. It is the whole of the
// metrics listener: docs/02 has it serve /metrics and nothing else.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/metrics" {
			http.NotFound(w, req)
			return
		}
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var b strings.Builder
		if err := r.WriteText(&b); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", ContentType)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(b.Len()))
		if req.Method == http.MethodHead {
			return
		}
		_, _ = io.WriteString(w, b.String())
	})
}

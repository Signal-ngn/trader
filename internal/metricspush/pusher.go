// Package metricspush implements a Grafana Cloud remote_write push client.
// It collects all registered Prometheus metrics from the default registry
// and forwards them to a Grafana Cloud Prometheus remote_write endpoint every
// 30 seconds using the Prometheus remote_write v1 protocol (protobuf + snappy).
//
// Usage:
//
//	p := metricspush.New(metricspush.Config{
//	    URL:      "https://prometheus-prod-xx.grafana.net/api/prom/push",
//	    Username: "123456",
//	    APIKey:   "glc_xxx...",
//	})
//	go p.Start(ctx)
package metricspush

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	dto "github.com/prometheus/client_model/go"

	"github.com/klauspost/compress/s2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
)

const (
	defaultInterval = 30 * time.Second
	remoteWriteVersion = "0.1.0"
)

// Config holds configuration for the Grafana Cloud push client.
type Config struct {
	// URL is the Prometheus remote_write endpoint.
	// e.g. "https://prometheus-prod-01-eu-west-0.grafana.net/api/prom/push"
	URL string

	// Username is the Grafana Cloud metrics user ID (numeric string).
	Username string

	// APIKey is the Grafana Cloud API key used as HTTP Basic Auth password.
	APIKey string

	// Interval between pushes. Defaults to 30 seconds.
	Interval time.Duration

	// Gatherer is the Prometheus registry to collect from.
	// Defaults to prometheus.DefaultGatherer.
	Gatherer prometheus.Gatherer
}

// Pusher collects Prometheus metrics and pushes them to Grafana Cloud.
type Pusher struct {
	cfg      Config
	client   *http.Client
}

// New creates a new Pusher with the given config.
func New(cfg Config) *Pusher {
	if cfg.Interval == 0 {
		cfg.Interval = defaultInterval
	}
	if cfg.Gatherer == nil {
		cfg.Gatherer = prometheus.DefaultGatherer
	}
	return &Pusher{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Start runs the push loop until ctx is cancelled. It blocks until the loop
// exits, so callers should run it in a goroutine.
func (p *Pusher) Start(ctx context.Context) {
	log.Info().
		Str("url", p.cfg.URL).
		Dur("interval", p.cfg.Interval).
		Msg("grafana cloud metrics push started")

	ticker := time.NewTicker(p.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("grafana cloud metrics push stopped")
			return
		case <-ticker.C:
			if err := p.push(ctx); err != nil {
				log.Warn().Err(err).Msg("grafana cloud metrics push failed")
			}
		}
	}
}

// push gathers all metrics and sends one WriteRequest to Grafana Cloud.
func (p *Pusher) push(ctx context.Context) error {
	mfs, err := p.cfg.Gatherer.Gather()
	if err != nil {
		return fmt.Errorf("gather metrics: %w", err)
	}

	now := time.Now().UnixMilli()
	payload, err := encodeWriteRequest(mfs, now)
	if err != nil {
		return fmt.Errorf("encode write request: %w", err)
	}

	compressed := s2.EncodeSnappy(nil, payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.URL, bytes.NewReader(compressed))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("X-Prometheus-Remote-Write-Version", remoteWriteVersion)
	req.SetBasicAuth(p.cfg.Username, p.cfg.APIKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("remote_write returned %d: %s", resp.StatusCode, body)
	}

	log.Debug().
		Int("metric_families", len(mfs)).
		Int("payload_bytes", len(payload)).
		Int("compressed_bytes", len(compressed)).
		Msg("grafana cloud metrics pushed")

	return nil
}

// ── Minimal Prometheus remote_write protobuf encoding ──────────────────────
//
// We hand-encode the WriteRequest protobuf using raw wire format to avoid
// pulling in the full prometheus/prometheus module. The schema is:
//
//	message WriteRequest { repeated TimeSeries timeseries = 1; }
//	message TimeSeries   { repeated Label labels = 1; repeated Sample samples = 2; }
//	message Label        { string name = 1; string value = 2; }
//	message Sample       { double value = 1; int64 timestamp = 2; }
//
// Wire types: 0=varint, 1=64-bit, 2=length-delimited, 5=32-bit.

func encodeWriteRequest(mfs []*dto.MetricFamily, timestampMs int64) ([]byte, error) {
	var buf bytes.Buffer
	for _, mf := range mfs {
		tss := metricFamilyToTimeSeries(mf, timestampMs)
		for _, ts := range tss {
			tsBytes := encodeTimeSeries(ts)
			// field 1 (timeseries), wire type 2 (length-delimited)
			appendTag(&buf, 1, 2)
			appendVarint(&buf, uint64(len(tsBytes)))
			buf.Write(tsBytes)
		}
	}
	return buf.Bytes(), nil
}

type label struct{ name, value string }
type sample struct {
	value     float64
	timestamp int64
}
type timeSeries struct {
	labels  []label
	samples []sample
}

func encodeTimeSeries(ts timeSeries) []byte {
	var buf bytes.Buffer
	for _, l := range ts.labels {
		lBytes := encodeLabel(l)
		appendTag(&buf, 1, 2)
		appendVarint(&buf, uint64(len(lBytes)))
		buf.Write(lBytes)
	}
	for _, s := range ts.samples {
		sBytes := encodeSample(s)
		appendTag(&buf, 2, 2)
		appendVarint(&buf, uint64(len(sBytes)))
		buf.Write(sBytes)
	}
	return buf.Bytes()
}

func encodeLabel(l label) []byte {
	var buf bytes.Buffer
	appendTag(&buf, 1, 2)
	appendVarint(&buf, uint64(len(l.name)))
	buf.WriteString(l.name)
	appendTag(&buf, 2, 2)
	appendVarint(&buf, uint64(len(l.value)))
	buf.WriteString(l.value)
	return buf.Bytes()
}

func encodeSample(s sample) []byte {
	var buf bytes.Buffer
	// field 1, wire type 1 (64-bit / double)
	appendTag(&buf, 1, 1)
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], math.Float64bits(s.value))
	buf.Write(b[:])
	// field 2, wire type 0 (varint / int64 — positive timestamps, so uint64 cast is fine)
	appendTag(&buf, 2, 0)
	appendVarint(&buf, uint64(s.timestamp))
	return buf.Bytes()
}

func appendTag(buf *bytes.Buffer, field, wireType uint64) {
	appendVarint(buf, (field<<3)|wireType)
}

func appendVarint(buf *bytes.Buffer, v uint64) {
	for v >= 0x80 {
		buf.WriteByte(byte(v&0x7f) | 0x80)
		v >>= 7
	}
	buf.WriteByte(byte(v))
}

// ── Metric family → TimeSeries conversion ──────────────────────────────────

func metricFamilyToTimeSeries(mf *dto.MetricFamily, timestampMs int64) []timeSeries {
	name := mf.GetName()
	var result []timeSeries

	for _, m := range mf.GetMetric() {
		baseLabels := protoLabels(name, m)
		switch mf.GetType() {
		case dto.MetricType_COUNTER:
			if c := m.GetCounter(); c != nil {
				result = append(result, timeSeries{
					labels:  baseLabels,
					samples: []sample{{value: c.GetValue(), timestamp: timestampMs}},
				})
			}
		case dto.MetricType_GAUGE:
			if g := m.GetGauge(); g != nil {
				result = append(result, timeSeries{
					labels:  baseLabels,
					samples: []sample{{value: g.GetValue(), timestamp: timestampMs}},
				})
			}
		case dto.MetricType_UNTYPED:
			if u := m.GetUntyped(); u != nil {
				result = append(result, timeSeries{
					labels:  baseLabels,
					samples: []sample{{value: u.GetValue(), timestamp: timestampMs}},
				})
			}
		case dto.MetricType_SUMMARY:
			if s := m.GetSummary(); s != nil {
				// Emit _sum and _count
				result = append(result,
					timeSeries{labels: withSuffix(baseLabels, name, "_sum"), samples: []sample{{value: s.GetSampleSum(), timestamp: timestampMs}}},
					timeSeries{labels: withSuffix(baseLabels, name, "_count"), samples: []sample{{value: float64(s.GetSampleCount()), timestamp: timestampMs}}},
				)
				for _, q := range s.GetQuantile() {
					ql := append(copyLabels(baseLabels), label{name: "quantile", value: fmt.Sprintf("%g", q.GetQuantile())})
					result = append(result, timeSeries{labels: ql, samples: []sample{{value: q.GetValue(), timestamp: timestampMs}}})
				}
			}
		case dto.MetricType_HISTOGRAM:
			if h := m.GetHistogram(); h != nil {
				result = append(result,
					timeSeries{labels: withSuffix(baseLabels, name, "_sum"), samples: []sample{{value: h.GetSampleSum(), timestamp: timestampMs}}},
					timeSeries{labels: withSuffix(baseLabels, name, "_count"), samples: []sample{{value: float64(h.GetSampleCount()), timestamp: timestampMs}}},
				)
				for _, b := range h.GetBucket() {
					bl := append(copyLabels(baseLabels), label{name: "le", value: fmt.Sprintf("%g", b.GetUpperBound())})
					result = append(result, timeSeries{labels: bl, samples: []sample{{value: float64(b.GetCumulativeCount()), timestamp: timestampMs}}})
				}
			}
		}
	}
	return result
}

// protoLabels builds the sorted label set for a metric, including __name__.
func protoLabels(metricName string, m *dto.Metric) []label {
	labels := []label{{name: "__name__", value: metricName}}
	for _, lp := range m.GetLabel() {
		labels = append(labels, label{name: lp.GetName(), value: lp.GetValue()})
	}
	return labels
}

// withSuffix returns a copy of labels with __name__ replaced by name+suffix.
func withSuffix(labels []label, name, suffix string) []label {
	out := make([]label, len(labels))
	copy(out, labels)
	for i, l := range out {
		if l.name == "__name__" {
			out[i].value = name + suffix
			break
		}
	}
	return out
}

func copyLabels(labels []label) []label {
	out := make([]label, len(labels))
	copy(out, labels)
	return out
}

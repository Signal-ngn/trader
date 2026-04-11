package engine

import (
	"math"

	"github.com/Signal-ngn/risk"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	metricsNamespace  = "spot_canvas"
	metricsSubsystem  = "conviction"
)

var (
	convictionScore = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "score",
			Help:      "Latest conviction score for a position (0.0-1.0)",
		},
		[]string{"symbol"},
	)

	convictionErosionSignal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "erosion_signal_active",
			Help:      "Active erosion signal weight (0 = inactive)",
		},
		[]string{"symbol", "signal"},
	)

	convictionActionsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "actions_total",
			Help:      "Total conviction-triggered actions (exit, tighten)",
		},
		[]string{"symbol", "action"},
	)

	convictionWarmupGauge = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "warmup",
			Help:      "1 if scorer is still warming up, 0 if warm",
		},
		[]string{"symbol"},
	)
)

// allErosionSignals is the exhaustive set of signal names from risk.ConvictionScorer.
var allErosionSignals = []string{
	"macd_histogram_decline",
	"rsi_cross",
	"stochastic_cross",
	"supertrend_flip",
	"atr_expansion",
	"bollinger_rejection",
	"adverse_drawdown",
	"signal_staleness",
}

func recordConvictionScore(symbol string, score float64, signals []risk.ErosionSignal) {
	convictionScore.WithLabelValues(symbol).Set(score)

	active := make(map[string]float64, len(signals))
	for _, s := range signals {
		active[s.Name] = math.Abs(s.Weight)
	}

	for _, name := range allErosionSignals {
		if w, ok := active[name]; ok {
			convictionErosionSignal.WithLabelValues(symbol, name).Set(w)
		} else {
			convictionErosionSignal.WithLabelValues(symbol, name).Set(0)
		}
	}
}

func recordConvictionAction(symbol, action string) {
	convictionActionsTotal.WithLabelValues(symbol, action).Inc()
}

func recordConvictionWarmup(symbol string, warming bool) {
	v := 0.0
	if warming {
		v = 1.0
	}
	convictionWarmupGauge.WithLabelValues(symbol).Set(v)
}

func cleanupConvictionMetrics(symbol string) {
	convictionScore.DeleteLabelValues(symbol)
	convictionWarmupGauge.DeleteLabelValues(symbol)
	for _, name := range allErosionSignals {
		convictionErosionSignal.DeleteLabelValues(symbol, name)
	}
}

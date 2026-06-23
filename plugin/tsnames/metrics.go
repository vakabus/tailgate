package tailscale

import (
	"github.com/coredns/coredns/plugin"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Variables declared for monitoring.
var (
	requestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "tsnames",
		Name:      "requests_total",
		Help:      "Counter of DNS requests handled by the tsnames plugin.",
	}, []string{"zone", "qtype", "result"})

	requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:                   plugin.Namespace,
		Subsystem:                   "tsnames",
		Name:                        "request_duration_seconds",
		NativeHistogramBucketFactor: plugin.NativeHistogramBucketFactor,
		Help:                        "Histogram of the time (in seconds) each DNS request took.",
	}, []string{"zone", "qtype"})

	entriesGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: plugin.Namespace,
		Subsystem: "tsnames",
		Name:      "entries",
		Help:      "Number of Tailscale hostname entries currently tracked.",
	}, []string{"zone"})

	netmapUpdatesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "tsnames",
		Name:      "netmap_updates_total",
		Help:      "Counter of Tailscale netmap updates processed.",
	}, []string{"zone"})

	busReconnectsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "tsnames",
		Name:      "bus_reconnects_total",
		Help:      "Counter of Tailscale IPN bus reconnects (connect failures or mid-stream errors). A rapidly climbing rate indicates a reconnect storm.",
	}, []string{"zone"})
)

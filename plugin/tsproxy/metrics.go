package tsproxy

import (
	"github.com/coredns/coredns/plugin"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Variables declared for monitoring.
var (
	connectionsCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "tsproxy",
		Name:      "connections_total",
		Help:      "Counter of connections handled (TCP: per accepted connection, UDP: per client session, https_redirect: per request).",
	}, []string{"protocol", "listen_port", "target"})

	proxiedBytesCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "tsproxy",
		Name:      "proxied_bytes_total",
		Help:      "Counter of bytes proxied, by direction (up: client->target, down: target->client).",
	}, []string{"protocol", "listen_port", "target", "direction"})

	connectionDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:                   plugin.Namespace,
		Subsystem:                   "tsproxy",
		Name:                        "connection_duration_seconds",
		NativeHistogramBucketFactor: plugin.NativeHistogramBucketFactor,
		Help:                        "Histogram of the lifetime of each connection/session.",
	}, []string{"protocol", "listen_port", "target"})

	connectionBytes = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:                   plugin.Namespace,
		Subsystem:                   "tsproxy",
		Name:                        "connection_bytes",
		NativeHistogramBucketFactor: plugin.NativeHistogramBucketFactor,
		Help:                        "Histogram of the total bytes (up+down) proxied per connection/session.",
	}, []string{"protocol", "listen_port", "target"})

	activeConnections = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: plugin.Namespace,
		Subsystem: "tsproxy",
		Name:      "active_connections",
		Help:      "Gauge of currently open connections/sessions.",
	}, []string{"protocol", "listen_port", "target"})
)

const (
	directionUp   = "up"
	directionDown = "down"
)

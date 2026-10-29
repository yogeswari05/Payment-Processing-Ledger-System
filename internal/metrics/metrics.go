package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	HTTPRequestsTotal     *prometheus.CounterVec
	HTTPRequestDuration   *prometheus.HistogramVec
	PaymentsTotal         *prometheus.CounterVec
	PaymentAmountTotal    *prometheus.CounterVec
	OutboxEventsTotal     *prometheus.CounterVec
	ReconciliationRuns    *prometheus.CounterVec
	ReconciliationAnomalies *prometheus.CounterVec
}

func New() *Metrics {
	return &Metrics{
		HTTPRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "payflow",
				Subsystem: "http",
				Name:      "requests_total",
				Help:      "Total count of HTTP requests processed partitioned by method, handler, and response status code.",
			},
			[]string{"method", "handler", "code"},
		),
		HTTPRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "payflow",
				Subsystem: "http",
				Name:      "request_duration_seconds",
				Help:      "HTTP request latencies in seconds.",
				Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
			},
			[]string{"method", "handler"},
		),
		PaymentsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "payflow",
				Subsystem: "payment",
				Name:      "operations_total",
				Help:      "Total count of payment operations partitioned by currency and status.",
			},
			[]string{"currency", "status"},
		),
		PaymentAmountTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "payflow",
				Subsystem: "payment",
				Name:      "amount_minor_units_total",
				Help:      "Total monetary volume in minor units processed by PayFlow.",
			},
			[]string{"currency", "status"},
		),
		OutboxEventsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "payflow",
				Subsystem: "outbox",
				Name:      "events_total",
				Help:      "Total count of outbox events processed partitioned by event type and status.",
			},
			[]string{"event_type", "status"},
		),
		ReconciliationRuns: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "payflow",
				Subsystem: "reconciliation",
				Name:      "runs_total",
				Help:      "Total count of reconciliation audit runs partitioned by final status.",
			},
			[]string{"status"},
		),
		ReconciliationAnomalies: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "payflow",
				Subsystem: "reconciliation",
				Name:      "anomalies_detected_total",
				Help:      "Total count of data anomalies detected by reconciliation checks.",
			},
			[]string{"anomaly_type"},
		),
	}
}

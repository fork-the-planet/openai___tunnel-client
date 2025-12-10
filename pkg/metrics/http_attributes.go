package metrics

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
)

// MetricAttributesForRequest derives common HTTP attributes for Prometheus/OpenTelemetry metrics.
func MetricAttributesForRequest(req *http.Request) []attribute.KeyValue {
	if req == nil || req.URL == nil {
		return nil
	}

	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}

	return []attribute.KeyValue{
		attribute.String("http.route", path),
	}
}

// WithHTTPClientMetricAttributesFn wraps MetricAttributesForRequest for otelhttp transports.
func WithHTTPClientMetricAttributesFn() otelhttp.Option {
	return otelhttp.WithMetricAttributesFn(MetricAttributesForRequest)
}

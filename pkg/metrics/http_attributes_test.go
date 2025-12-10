package metrics

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMetricAttributesForRequest(t *testing.T) {
	t.Run("nil request", func(t *testing.T) {
		require.Nil(t, MetricAttributesForRequest(nil))
	})

	t.Run("nil URL", func(t *testing.T) {
		req := &http.Request{}
		require.Nil(t, MetricAttributesForRequest(req))
	})

	t.Run("empty path coerces to slash", func(t *testing.T) {
		req := &http.Request{URL: &url.URL{}}
		attrs := MetricAttributesForRequest(req)

		require.Len(t, attrs, 1)
		require.Equal(t, "http.route", string(attrs[0].Key))
		require.Equal(t, "/", attrs[0].Value.AsString())
	})

	t.Run("non-empty path uses escaped path", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "https://example.com/v1/hello%20world", nil)
		attrs := MetricAttributesForRequest(req)

		require.Len(t, attrs, 1)
		require.Equal(t, "http.route", string(attrs[0].Key))
		require.Equal(t, "/v1/hello%20world", attrs[0].Value.AsString())
	})
}

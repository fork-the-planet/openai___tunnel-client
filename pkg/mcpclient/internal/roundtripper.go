package internal

import "net/http"

// ForwardingRoundTripper decorates the base RoundTripper so it can read request
// headers from the context and capture the response headers for later use.
type ForwardingRoundTripper struct {
	base http.RoundTripper
}

// NewForwardingRoundTripper constructs a RoundTripper that forwards headers to
// the underlying transport. When base is nil, http.DefaultTransport is used.
func NewForwardingRoundTripper(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		panic("nil base RoundTripper")
	}
	return &ForwardingRoundTripper{
		base: base,
	}
}

// RoundTrip injects headers before issuing the request and records the response
// headers after the call returns.
func (f *ForwardingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	carrier := CarrierFromContext(req.Context())
	if carrier != nil {
		carrier.ApplyRequestHeaders(req.Header)
	}
	resp, err := f.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	if carrier != nil {
		carrier.StoreResponse(resp.StatusCode, resp.Header)
	}
	return resp, nil
}

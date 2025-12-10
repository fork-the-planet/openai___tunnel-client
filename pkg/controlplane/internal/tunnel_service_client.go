package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	otelmetric "go.opentelemetry.io/otel/metric"

	"go.openai.org/api/tunnel-client/pkg/config"
	wiretypes "go.openai.org/api/tunnel-client/pkg/controlplane/wiretypes"
	tclog "go.openai.org/api/tunnel-client/pkg/log"
	"go.openai.org/api/tunnel-client/pkg/mcpclient"
	tcmetrics "go.openai.org/api/tunnel-client/pkg/metrics"
	"go.openai.org/api/tunnel-client/pkg/tunnelctx"
	"go.openai.org/api/tunnel-client/pkg/types"
	"go.openai.org/api/tunnel-client/pkg/version"
)

const (
	defaultPollTimeout = 30 * time.Second
	pollPathFormat     = "/v1/tunnel/%s/poll"
	responsePathFormat = "/v1/tunnel/%s/response"
)

var errMissingConfig = errors.New("controlplane client: config is required")

// TunnelServiceClient implements the Fetcher and Responder interfaces backed by
// the control-plane HTTP API.
type TunnelServiceClient struct {
	client           *http.Client
	pollEndpoint     *url.URL
	responseEndpoint *url.URL
	logger           *slog.Logger
	tunnelID         types.TunnelID
	apiKey           string
	userAgent        string
}

// NewTunnelServiceClient constructs an HTTP-backed client using the provided config.
func NewTunnelServiceClient(ctx context.Context, cfg *config.ControlPlaneConfig, logger *slog.Logger, loggingCfg *config.LoggingConfig, meterProvider otelmetric.MeterProvider) (*TunnelServiceClient, error) {
	if cfg == nil {
		return nil, errMissingConfig
	}
	if cfg.BaseURL == nil {
		return nil, errors.New("controlplane client: control-plane.base-url is required")
	}
	if cfg.TunnelID == "" {
		return nil, errors.New("controlplane client: control-plane.tunnel-id is required")
	}
	if cfg.APIKey == "" {
		return nil, errors.New("controlplane client: control-plane.api-key is required")
	}
	if meterProvider == nil {
		return nil, errors.New("controlplane client: meter provider is required")
	}

	if logger == nil {
		return nil, errors.New("controlplane client: logger is required")
	}

	tunnelIDSegment := url.PathEscape(cfg.TunnelID.String())
	pollEndpoint := cfg.BaseURL.ResolveReference(&url.URL{Path: fmt.Sprintf(pollPathFormat, tunnelIDSegment)})
	responseEndpoint := cfg.BaseURL.ResolveReference(&url.URL{Path: fmt.Sprintf(responsePathFormat, tunnelIDSegment)})

	timeout := cfg.PollTimeout
	if timeout <= 0 {
		timeout = defaultPollTimeout
	}

	transport := buildControlPlaneHTTPTransport(cfg, logger, loggingCfg, meterProvider)

	client := &TunnelServiceClient{
		client: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
		pollEndpoint:     pollEndpoint,
		responseEndpoint: responseEndpoint,
		logger:           logger,
		tunnelID:         cfg.TunnelID,
		apiKey:           cfg.APIKey,
		userAgent:        version.UserAgent,
	}
	logger.InfoContext(ctx, "TunnelServiceClient created",
		slog.String("tunnel_id", client.tunnelID.String()),
		slog.String("poll_endpoint", client.pollEndpoint.String()),
		slog.String("response_endpoint", client.responseEndpoint.String()),
		slog.Int64("timeout_ms", timeout.Milliseconds()),
	)

	return client, nil
}

func buildControlPlaneHTTPTransport(cfg *config.ControlPlaneConfig, logger *slog.Logger, loggingCfg *config.LoggingConfig, meterProvider otelmetric.MeterProvider) http.RoundTripper {
	// Order matters (outermost to innermost):
	//   1. Control-plane round tripper applies auth headers before anything else.
	//   2. Logging wraps otel instrumentation so dumps include the final headers.
	//   3. otelhttp instrumentation sits closest to the network for accurate metrics.
	base := http.DefaultTransport
	base = otelhttp.NewTransport(
		base,
		otelhttp.WithMeterProvider(meterProvider),
		tcmetrics.WithHTTPClientMetricAttributesFn(),
	)
	base = tclog.NewRoundTripper(base, logger, loggingCfg, tclog.ComponentControlPlane)

	return newControlPlaneRoundTripper(
		base,
		cfg.APIKey,
		version.UserAgent,
		cfg.ExtraHeaders,
		logger,
	)
}

// PostResponse acknowledges the provided request with the JSON-RPC response.
func (c *TunnelServiceClient) PostResponse(ctx context.Context, requestID types.RequestID, response *types.TunnelResponse) (types.TunnelServiceRequestID, error) {
	if requestID == "" {
		return "", errors.New("controlplane responder: requestID is required")
	}
	if response == nil {
		return "", errors.New("controlplane responder: response is required")
	}

	if err := response.Validate(); err != nil {
		return "", fmt.Errorf("controlplane responder: %w", err)
	}

	rpcPayload := response.JSONRPC()

	var rawResponse json.RawMessage
	if rpcPayload != nil {
		encoded, err := jsonrpc.EncodeMessage(rpcPayload)
		if err != nil {
			return "", fmt.Errorf("controlplane responder: encode jsonrpc response: %w", err)
		}
		rawResponse = json.RawMessage(encoded)
	}

	payload := wiretypes.TunnelResponsePayload{
		RequestID:       requestID.String(),
		ResponseHeaders: response.Headers(),
		ResponseCode:    response.ResponseCode(),
		ResponseType:    wiretypes.ResponsePayloadJSONRPC,
	}
	if len(rawResponse) > 0 {
		payload.JSONRPCResponse = rawResponse
	}
	if response.Type() == types.ResponseTypeNotificationAcknowledgment {
		payload.ResponseType = wiretypes.ResponsePayloadNotifyAck
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("controlplane responder: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.responseEndpoint.String(), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("controlplane responder: build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if controlPlaneRequestID, ok := tunnelctx.ControlPlaneCommandRequestIDFromContext(ctx); ok {
		req.Header.Set("X-Client-Request-Id", controlPlaneRequestID.String())
	}
	shardToken, ok := tunnelctx.ShardTokenFromContext(ctx)
	if !ok || shardToken == "" {
		return "", errors.New("controlplane responder: shard token is required")
	}
	req.Header.Set("X-Tunnel-Shard-Token", shardToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("controlplane responder: post response: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	var tunnelServiceRequestID types.TunnelServiceRequestID
	if id, ok := types.NewTunnelServiceRequestIDFromHeader(resp.Header); ok {
		tunnelServiceRequestID = id
		ctx = tunnelctx.ContextWithTunnelServiceRequestID(ctx, tunnelServiceRequestID)
	}

	ctx = tunnelctx.ContextWithRequestID(ctx, requestID.String())
	logger := tclog.LoggerWithContextIdentifiers(ctx, c.logger)
	switch resp.StatusCode {
	case http.StatusOK:
		logger.DebugContext(ctx, "posted response to control-plane")
		return tunnelServiceRequestID, nil
	case http.StatusNotFound:
		logger.WarnContext(ctx, "response already fulfilled or unknown request")
		return tunnelServiceRequestID, nil
	default:
		_, _ = io.Copy(io.Discard, resp.Body)
		return tunnelServiceRequestID, fmt.Errorf("controlplane responder: unexpected status %d", resp.StatusCode)
	}
}

// Poll requests up to limit commands from the control plane.
func (c *TunnelServiceClient) Poll(ctx context.Context, limit int) ([]PolledCommand, types.TunnelServiceRequestID, error) {
	if limit <= 0 {
		return nil, "", nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.pollEndpoint.String(), nil)
	if err != nil {
		return nil, "", err
	}

	query := req.URL.Query()
	query.Set("limit", strconv.Itoa(limit))
	req.URL.RawQuery = query.Encode()

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	var tunnelServiceRequestID types.TunnelServiceRequestID
	if id, ok := types.NewTunnelServiceRequestIDFromHeader(resp.Header); ok {
		tunnelServiceRequestID = id
		ctx = tunnelctx.ContextWithTunnelServiceRequestID(ctx, tunnelServiceRequestID)
	}

	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil, tunnelServiceRequestID, nil
	case http.StatusOK:
		cmds, err := c.decodeCommands(ctx, resp.Body, limit)
		return cmds, tunnelServiceRequestID, err
	default:
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, tunnelServiceRequestID, fmt.Errorf("controlplane client: unexpected status %d", resp.StatusCode)
	}
}

func (c *TunnelServiceClient) decodeCommands(ctx context.Context, r io.Reader, limit int) ([]PolledCommand, error) {
	limited := limit
	if limited <= 0 {
		limited = 1
	}

	polledAt := time.Now()

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("controlplane client: read poll response: %w", err)
	}

	var envelope wiretypes.PolledCommandEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("controlplane client: decode poll response: %w", err)
	}
	commands := envelope.Commands

	if len(commands) == 0 {
		return nil, nil
	}

	if len(commands) > limited {
		commands = commands[:limited]
	}

	logger := tclog.LoggerWithContextIdentifiers(ctx, c.logger)
	out := make([]PolledCommand, 0, len(commands))
	for _, raw := range commands {
		cmd, err := convertRawCommand(raw, polledAt)
		if err != nil {
			logger.WarnContext(ctx, "control-plane command dropped: invalid payload", slog.String(tclog.FieldRequestID, raw.RequestID), slog.String("error", err.Error()))
			continue
		}
		out = append(out, cmd)
	}

	return out, nil
}

type polledCommand struct {
	requestID  types.RequestID
	message    jsonrpc.Message
	enqueued   time.Time
	polledAt   time.Time
	headers    http.Header
	sessionID  *string
	shardToken string
}

func convertRawCommand(raw wiretypes.RawPolledCommand, polledAt time.Time) (*polledCommand, error) {
	if raw.RequestID == "" {
		return nil, errors.New("missing request_id")
	}
	if raw.ShardToken == "" {
		return nil, errors.New("missing shard_token")
	}
	// Ensure JSON is non-empty; empty object is acceptable.
	if len(raw.JSONRPC) == 0 {
		return nil, errors.New("missing jsonrpc payload")
	}

	msg, err := jsonrpc.DecodeMessage(raw.JSONRPC)
	if err != nil {
		return nil, fmt.Errorf("invalid jsonrpc payload: %w", err)
	}

	timestamp := raw.CreatedAt

	enqueuedAt := time.Time{}
	if timestamp != "" {
		parsed, err := time.Parse(time.RFC3339Nano, timestamp)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, timestamp)
			if err != nil {
				return nil, fmt.Errorf("invalid enqueued_at: %w", err)
			}
		}
		enqueuedAt = parsed
	}
	headers := make(http.Header)
	// this will put headers in canonical form
	if raw.Headers != nil {
		for key, values := range raw.Headers {
			for _, value := range values {
				headers.Add(key, value)
			}
		}
	}

	return &polledCommand{
		requestID:  types.RequestID(raw.RequestID),
		message:    msg,
		enqueued:   enqueuedAt,
		polledAt:   polledAt,
		headers:    headers,
		shardToken: raw.ShardToken,
		sessionID:  mcpclient.SessionIDFromHeaders(headers),
	}, nil
}

func (c *polledCommand) RequestID() types.RequestID {
	return c.requestID
}

func (c *polledCommand) Message() jsonrpc.Message {
	return c.message
}

func (c *polledCommand) EnqueuedAt() time.Time {
	return c.enqueued
}

func (c *polledCommand) PolledAt() time.Time {
	return c.polledAt
}

func (c *polledCommand) Headers() http.Header {
	if c.headers == nil {
		return nil
	}
	return c.headers
}

func (c *polledCommand) ShardToken() string {
	return c.shardToken
}

func (c *polledCommand) SessionID() (string, bool) {
	if c.sessionID == nil {
		return "", false
	}
	return *c.sessionID, true
}

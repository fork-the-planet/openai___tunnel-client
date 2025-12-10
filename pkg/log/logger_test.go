package log_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	tclog "go.openai.org/api/tunnel-client/pkg/log"
	"go.openai.org/api/tunnel-client/pkg/tunnelctx"
	"go.openai.org/api/tunnel-client/pkg/types"
)

func TestLoggingContextHelpers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx = tunnelctx.ContextWithRequestID(ctx, "req-456")
	ctx = tunnelctx.ContextWithSessionID(ctx, "session-123")
	ctx = tunnelctx.ContextWithControlPlaneCommandRequestID(ctx, types.ControlPlaneRequestID("control-plane-req-789"))
	ctx = tunnelctx.ContextWithTunnelServiceRequestID(ctx, types.TunnelServiceRequestID("tunnel-service-req-456"))
	id, err := jsonrpc.MakeID(float64(12))
	if err != nil {
		t.Fatalf("make id: %v", err)
	}
	ctx = tunnelctx.ContextWithRPCRequestID(ctx, id)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	logger = tclog.LoggerWithContextIdentifiers(ctx, logger)
	logger.InfoContext(ctx, "test message")

	if !strings.Contains(buf.String(), "session_id=session-123") {
		t.Fatalf("expected session attribute in logs, got: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "request_id=req-456") {
		t.Fatalf("expected request attribute in logs, got: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "cmd_request_id=control-plane-req-789") {
		t.Fatalf("expected control plane command request attribute in logs, got: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "tunnel_request_id=tunnel-service-req-456") {
		t.Fatalf("expected tunnel service request attribute in logs, got: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "rpc_request_id=12") {
		t.Fatalf("expected rpc request attribute in logs, got: %s", buf.String())
	}

	t.Run("request only", func(t *testing.T) {
		ctx := tunnelctx.ContextWithRequestID(context.Background(), "only-req")
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, nil))
		logger = tclog.LoggerWithContextIdentifiers(ctx, logger)
		logger.InfoContext(ctx, "request only")

		if !strings.Contains(buf.String(), "request_id=only-req") {
			t.Fatalf("expected request attribute in logs, got: %s", buf.String())
		}
		if strings.Contains(buf.String(), "session_id") {
			t.Fatalf("did not expect session attribute in logs, got: %s", buf.String())
		}
		if strings.Contains(buf.String(), "rpc_request_id") {
			t.Fatalf("did not expect rpc request attribute in logs, got: %s", buf.String())
		}
	})

	t.Run("string rpc id", func(t *testing.T) {
		strID, err := jsonrpc.MakeID("rpc-abc")
		if err != nil {
			t.Fatalf("make id: %v", err)
		}
		ctx := tunnelctx.ContextWithRPCRequestID(context.Background(), strID)
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, nil))
		logger = tclog.LoggerWithContextIdentifiers(ctx, logger)
		logger.InfoContext(ctx, "string rpc id")

		if !strings.Contains(buf.String(), "rpc_request_id=rpc-abc") {
			t.Fatalf("expected rpc request attribute in logs, got: %s", buf.String())
		}
	})
}

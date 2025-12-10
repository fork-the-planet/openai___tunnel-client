package types

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/stretchr/testify/require"
)

func TestTunnelResponseValidateJSONRPC(t *testing.T) {
	t.Run("valid response", func(t *testing.T) {
		tr := NewTunnelResponse(&jsonrpc.Response{}, 200, nil)
		require.NoError(t, tr.Validate())
	})

	t.Run("missing payload", func(t *testing.T) {
		tr := &TunnelResponse{
			responseType: ResponseTypeJSONRPCResponse,
		}
		err := tr.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "jsonrpc response is required")
	})
}

func TestTunnelResponseValidateNotificationAck(t *testing.T) {
	t.Run("valid ack", func(t *testing.T) {
		require.NoError(t, NewNotificationAck(204, nil).Validate())
	})

	t.Run("ack with payload", func(t *testing.T) {
		tr := &TunnelResponse{
			responseType: ResponseTypeNotificationAcknowledgment,
			response:     &jsonrpc.Response{},
		}
		err := tr.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "must not include a jsonrpc response")
	})
}

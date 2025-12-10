package internal_test

import (
	"testing"

	"go.openai.org/api/tunnel-client/pkg/controlplane"
	internal "go.openai.org/api/tunnel-client/pkg/controlplane/internal"
)

func TestPolledCommandInterfaceParity(t *testing.T) {
	t.Helper()

	// assert that internal.PolledCommand implements controlplane.PolledCommand and vice versa
	var (
		_ controlplane.PolledCommand = (internal.PolledCommand)(nil)
		_ internal.PolledCommand     = (controlplane.PolledCommand)(nil)
	)
}

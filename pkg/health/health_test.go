package health

import (
	"net"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildHealthURLAssignsRandomPort(t *testing.T) {
	t.Parallel()

	t.Run("UnspecifiedHostDefaultsToLocalhost", func(t *testing.T) {
		t.Parallel()

		ln := listen(t, "tcp", ":0")
		defer func() {
			require.NoError(t, ln.Close())
		}()

		healthURL := mustBuildHealthURL(t, ":0", ln.Addr())
		parsed := parseURL(t, healthURL)

		require.Equal(t, "localhost", parsed.Hostname())
		require.Equal(t, portString(t, ln.Addr()), parsed.Port())
	})

	t.Run("IPv4Loopback", func(t *testing.T) {
		t.Parallel()

		ln := listen(t, "tcp4", "127.0.0.1:0")
		defer func() {
			require.NoError(t, ln.Close())
		}()

		healthURL := mustBuildHealthURL(t, "127.0.0.1:0", ln.Addr())
		parsed := parseURL(t, healthURL)

		require.Equal(t, "127.0.0.1", parsed.Hostname())
		require.Equal(t, portString(t, ln.Addr()), parsed.Port())
	})

	t.Run("IPv6Loopback", func(t *testing.T) {
		ln, err := net.Listen("tcp6", "[::1]:0")
		if err != nil {
			t.Skipf("ipv6 loopback not available: %v", err)
		}
		defer func() {
			require.NoError(t, ln.Close())
		}()

		healthURL := mustBuildHealthURL(t, "[::1]:0", ln.Addr())
		parsed := parseURL(t, healthURL)

		require.Equal(t, "::1", parsed.Hostname())
		require.Equal(t, portString(t, ln.Addr()), parsed.Port())
	})
}

func mustBuildHealthURL(t *testing.T, listenAddr string, addr net.Addr) string {
	t.Helper()

	healthURL, err := buildHealthURL(listenAddr, addr)
	require.NoError(t, err)
	return healthURL
}

func listen(t *testing.T, network, address string) net.Listener {
	t.Helper()

	ln, err := net.Listen(network, address)
	require.NoErrorf(t, err, "listen %s %s", network, address)
	return ln
}

func parseURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	parsed, err := url.Parse(raw)
	require.NoErrorf(t, err, "parse URL %s", raw)
	return parsed
}

func portString(t *testing.T, addr net.Addr) string {
	t.Helper()

	tcpAddr, ok := addr.(*net.TCPAddr)
	require.Truef(t, ok, "listener addr %T is not *net.TCPAddr", addr)
	require.NotZero(t, tcpAddr.Port, "listener should assign a random port")
	return strconv.Itoa(tcpAddr.Port)
}

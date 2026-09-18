package common

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnectionDiagnosticsCancellationOrigin(t *testing.T) {
	for _, localWriteTimeout := range []bool{false, true} {
		name := "peer_closed"
		if localWriteTimeout {
			name = "gateway_write_timeout"
		}
		t.Run(name, func(t *testing.T) {
			type observation struct {
				contextErr  error
				flushErr    error
				diagnostics ConnectionDiagnostics
				tracked     bool
			}
			entered := make(chan struct{})
			completed := make(chan observation, 1)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				var flushErr error
				if localWriteTimeout {
					controller := http.NewResponseController(w)
					flushErr = controller.SetWriteDeadline(time.Now().Add(-time.Second))
					if flushErr == nil {
						flushErr = controller.Flush()
					}
				}
				select {
				case <-r.Context().Done():
				case <-time.After(3 * time.Second):
				}
				diagnostics, tracked := GetConnectionDiagnostics(r.Context())
				completed <- observation{r.Context().Err(), flushErr, diagnostics, tracked}
			}))
			server.Listener = WithConnectionDiagnostics(server.Listener)
			server.Config.ConnContext = ConnectionDiagnosticsContext
			server.Start()
			t.Cleanup(server.Close)
			client, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
			require.NoError(t, err)
			defer client.Close()
			_, err = fmt.Fprint(client, "GET / HTTP/1.1\r\nHost: test\r\n\r\n")
			require.NoError(t, err)
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("request did not reach server")
			}
			if !localWriteTimeout {
				require.NoError(t, client.Close())
			}
			var result observation
			select {
			case result = <-completed:
			case <-time.After(5 * time.Second):
				t.Fatal("cancellation was not observed")
			}
			require.True(t, result.tracked)
			assert.ErrorIs(t, result.contextErr, context.Canceled, "both causes produce the same request context error")
			if localWriteTimeout {
				require.Error(t, result.flushErr)
				assert.Equal(t, "timeout", result.diagnostics.WriteFailure.Kind)
				assert.False(t, result.diagnostics.WriteFailure.Deadline.IsZero())
				assert.NotContains(t, result.diagnostics.WriteFailure.Error, "127.0.0.1")
			} else {
				assert.Equal(t, "peer_eof", result.diagnostics.ReadFailure.Kind)
				assert.True(t, result.diagnostics.WriteFailure.At.IsZero())
			}
		})
	}
}

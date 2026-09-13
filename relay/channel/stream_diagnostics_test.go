package channel

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientDisconnectLogIncludesAdminConnectionDiagnostics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	service.InitHttpClient()
	upstreamCanceled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Request-Id", "upstream-trace-123")
		w.Header().Set("Authorization", "Bearer upstream-secret")
		_, _ = io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(upstreamCanceled)
	}))
	t.Cleanup(upstream.Close)

	clientContext, cancelClient := context.WithCancel(context.Background())
	t.Cleanup(cancelClient)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions?key=client-secret", strings.NewReader("private prompt")).WithContext(clientContext)
	c.Request.Header.Set("User-Agent", "ExampleClient/1.0")
	c.Request.Header.Set("Authorization", "Bearer client-secret")
	req, err := http.NewRequest(http.MethodPost, upstream.URL+"/?key=upstream-secret", strings.NewReader("private prompt"))
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{
		StartTime: time.Now(), IsStream: true, DisablePing: true,
		TokenKey: "client-secret", ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "upstream-secret"},
	}
	resp, err := doRequest(c, req, info)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	helper.StreamScannerHandler(c, resp, info, func(data string, _ *helper.StreamResult) {
		_ = helper.StringData(c, data)
		cancelClient()
	})
	select {
	case <-upstreamCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("the disconnected client's upstream request was not canceled")
	}

	other := service.GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 1, 0, 1)
	var logged map[string]any
	require.NoError(t, common.UnmarshalJsonStr(other.JSONString(), &logged))
	admin, ok := logged["admin_info"].(map[string]any)
	require.True(t, ok)
	diagnostics, ok := admin["request_diagnostics"].(map[string]any)
	require.True(t, ok, "a canceled stream needs administrator diagnostics")
	assert.Equal(t, "context canceled", diagnostics["client_context_error"])
	assert.Equal(t, "ExampleClient/1.0", diagnostics["client_user_agent"])
	assert.Equal(t, "HTTP/1.1", diagnostics["client_protocol"])
	assert.Equal(t, float64(http.StatusOK), diagnostics["upstream_status"])
	assert.Equal(t, "upstream-trace-123", diagnostics["upstream_request_id"])
	assert.Equal(t, float64(1), diagnostics["received_events"])
	assert.NotNil(t, diagnostics["headers_elapsed_ms"])
	assert.NotNil(t, diagnostics["last_upstream_activity_ms"])
	assert.Equal(t, false, diagnostics["ping_enabled"])
	assert.NotContains(t, diagnostics, "upstream_read_error", "closing the upstream after cancellation is not an independent upstream error")
	assert.NotContains(t, logged, "request_diagnostics")
	assert.NotContains(t, other.JSONString(), "client-secret")
	assert.NotContains(t, other.JSONString(), "upstream-secret")
	assert.NotContains(t, other.JSONString(), "private prompt")
	assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
}

func TestUpstreamFailureLogDistinguishesCancellationAndDeadline(t *testing.T) {
	service.InitHttpClient()
	for _, reason := range []string{"canceled", "deadline_exceeded"} {
		t.Run(reason, func(t *testing.T) {
			upstreamContext, cancel := context.WithCancel(context.Background())
			if reason == "deadline_exceeded" {
				cancel()
				upstreamContext, cancel = context.WithDeadline(context.Background(), time.Unix(1, 0))
			}
			cancel()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader("{}"))
			req, err := http.NewRequestWithContext(upstreamContext, http.MethodPost, "http://127.0.0.1:1/?token=secret-query", strings.NewReader("{}"))
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{StartTime: time.Now(), ChannelMeta: &relaycommon.ChannelMeta{}}
			_, err = doRequest(c, req, info)
			require.Error(t, err)
			other := service.GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 1, 0, 1)
			var logged map[string]any
			require.NoError(t, common.UnmarshalJsonStr(other.JSONString(), &logged))
			admin := logged["admin_info"].(map[string]any)
			diagnostics, ok := admin["request_diagnostics"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, reason, diagnostics["upstream_error_kind"])
			assert.NotContains(t, diagnostics, "client_context_error")
			assert.NotContains(t, other.JSONString(), "secret-query")
		})
	}
}

func TestStreamDiagnosticsSeparatesUpstreamReadFailureFromNormalCompletion(t *testing.T) {
	service.InitHttpClient()
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	for _, truncated := range []bool{false, true} {
		t.Run(strconv.FormatBool(truncated), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				body := "data: first\n\n"
				w.Header().Set("Content-Type", "text/event-stream")
				if truncated {
					w.Header().Set("Content-Length", strconv.Itoa(len(body)+20))
				} else {
					body += "data: [DONE]\n\n"
				}
				_, _ = io.WriteString(w, body)
			}))
			t.Cleanup(upstream.Close)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader("{}"))
			req, err := http.NewRequest(http.MethodPost, upstream.URL, strings.NewReader("{}"))
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{
				StartTime: time.Now(), IsStream: true, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{},
			}
			resp, err := doRequest(c, req, info)
			require.NoError(t, err)
			t.Cleanup(func() { _ = resp.Body.Close() })
			helper.StreamScannerHandler(c, resp, info, func(data string, _ *helper.StreamResult) {
				_ = helper.StringData(c, data)
			})
			other := service.GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 1, 0, 1)
			var logged map[string]any
			require.NoError(t, common.UnmarshalJsonStr(other.JSONString(), &logged))
			admin := logged["admin_info"].(map[string]any)
			if !truncated {
				assert.NotContains(t, admin, "request_diagnostics")
				assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
				return
			}
			diagnostics, ok := admin["request_diagnostics"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "unexpected EOF", diagnostics["upstream_read_error"])
			assert.NotContains(t, diagnostics, "client_context_error")
			assert.Equal(t, relaycommon.StreamEndReasonScannerErr, info.StreamStatus.EndReason)
		})
	}
}

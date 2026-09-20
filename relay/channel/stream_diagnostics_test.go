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
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions?key=client-secret", strings.NewReader("private prompt")).WithContext(common.WithRequestTimeline(clientContext))
	c.Request.Header.Set("User-Agent", "ExampleClient/1.0")
	c.Request.Header.Set("Authorization", "Bearer client-secret")
	req, err := http.NewRequest(http.MethodPost, upstream.URL+"/?key=upstream-secret", strings.NewReader("private prompt"))
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{
		StartTime: time.Now(), RequestId: "gateway-trace-456", IsStream: true, DisablePing: true,
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
	assert.Equal(t, "gateway-trace-456", diagnostics["gateway_request_id"])
	assert.Equal(t, float64(3), diagnostics["diagnostics_version"])
	assert.Equal(t, "Asia/Shanghai (UTC+08:00)", diagnostics["timezone"])
	assert.NotEmpty(t, diagnostics["request_phases"])
	assert.NotNil(t, diagnostics["gateway_elapsed_ms"])
	assert.NotContains(t, diagnostics["recorded_at"], "Z")
	assert.Equal(t, "/v1/chat/completions", diagnostics["request_path"])
	assert.Equal(t, false, diagnostics["usage_event_seen"])
	assert.Equal(t, false, diagnostics["terminal_event_seen"])
	assert.NotEmpty(t, diagnostics["upstream_body_closed_at"])
	assert.NotEmpty(t, diagnostics["last_upstream_data_at"])
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

func TestStreamDiagnosticMetadataIsBoundedAndExcludesContent(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := ": PING\n\n" + strings.Repeat(`data: {"type":"response.output_text.delta","delta":"private answer"}`+"\n\n", 12)
	body += `data: {"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":2}}}` + "\n\n"
	body += "data: [DONE]\n\n"
	info := &relaycommon.RelayInfo{StartTime: time.Now(), DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}
	helper.StreamScannerHandler(c, &http.Response{Body: io.NopCloser(strings.NewReader(body)), StatusCode: 200}, info, func(string, *helper.StreamResult) {})
	require.NotNil(t, info.StreamStatus.Diagnostics)
	assert.Len(t, info.StreamStatus.Diagnostics.RecentEvents, 8)
	assert.Equal(t, 1, info.StreamStatus.Diagnostics.CommentLines)
	assert.Positive(t, info.StreamStatus.Diagnostics.BlankLines)
	assert.True(t, info.StreamStatus.Diagnostics.UsageEventSeen)
	assert.True(t, info.StreamStatus.Diagnostics.TerminalEventSeen)
	// Even a normally terminated stream must retain evidence when billing validation fails.
	info.MissingBillableUsage = true
	other := service.GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 1, 0, 1)
	var logged map[string]any
	require.NoError(t, common.UnmarshalJsonStr(other.JSONString(), &logged))
	admin := logged["admin_info"].(map[string]any)
	diagnostics := admin["request_diagnostics"].(map[string]any)
	assert.Equal(t, true, diagnostics["missing_billable_usage"])
	assert.Len(t, diagnostics["recent_upstream_events"], 8)
	assert.NotContains(t, other.JSONString(), "private answer")
}

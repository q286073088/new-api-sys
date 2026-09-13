package service

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

func appendRelayRequestDiagnostics(ctx *gin.Context, info *relaycommon.RelayInfo, other *model.LogOther) {
	if info == nil || ctx.Request == nil {
		return
	}
	ss, upstream := info.StreamStatus, info.UpstreamDiagnostics
	clientErr := ctx.Request.Context().Err()
	streamFailed := ss != nil && (!ss.IsNormalEnd() || ss.HasErrors() || ss.EndError != nil)
	upstreamFailed := upstream != nil && (upstream.Err != nil || upstream.StatusCode >= 400)
	if !streamFailed && !upstreamFailed && clientErr == nil {
		return
	}

	diagnostics := map[string]any{
		"node_name":         common.NodeName,
		"client_protocol":   ctx.Request.Proto,
		"client_user_agent": relayDiagnosticText(info, ctx.Request.UserAgent()),
		"attempt_number":    info.RetryIndex + 1,
	}
	if !info.StartTime.IsZero() {
		diagnostics["request_elapsed_ms"] = max(0, time.Since(info.StartTime).Milliseconds())
	}
	if ctx.Request.ContentLength >= 0 {
		diagnostics["request_body_bytes"] = ctx.Request.ContentLength
	}
	if estimated := info.GetEstimatePromptTokens(); estimated > 0 {
		diagnostics["estimated_input_tokens"] = estimated
	}
	if ctx.Writer != nil {
		diagnostics["downstream_status"] = ctx.Writer.Status()
		diagnostics["downstream_written_bytes"] = max(0, ctx.Writer.Size())
	}
	if clientErr != nil {
		diagnostics["client_context_error"] = clientErr.Error()
	}
	if deadline, ok := ctx.Request.Context().Deadline(); ok {
		diagnostics["client_deadline"] = deadline.UTC().Format(time.RFC3339Nano)
	}
	if upstream != nil {
		diagnostics["upstream_host"] = upstream.Host
		diagnostics["relay_timeout_seconds"] = upstream.Timeout.Seconds()
		if upstream.StatusCode > 0 {
			diagnostics["upstream_status"] = upstream.StatusCode
			diagnostics["upstream_protocol"] = upstream.Protocol
			diagnostics["headers_elapsed_ms"] = max(0, upstream.CompletedAt.Sub(upstream.StartedAt).Milliseconds())
		}
		if upstream.RequestID != "" {
			diagnostics["upstream_request_id"] = relayDiagnosticText(info, upstream.RequestID)
		}
		if upstream.Err != nil {
			err := upstream.Err
			var urlErr *url.Error
			if errors.As(err, &urlErr) {
				err = urlErr.Err // The URL may carry credentials or signed query parameters.
			}
			diagnostics["upstream_error"] = relayDiagnosticText(info, err.Error())
			kind := "transport_error"
			var netErr net.Error
			switch {
			case errors.Is(err, context.Canceled):
				kind = "canceled"
			case errors.Is(err, context.DeadlineExceeded):
				kind = "deadline_exceeded"
			case errors.As(err, &netErr) && netErr.Timeout():
				kind = "timeout"
			}
			diagnostics["upstream_error_kind"] = kind
		}
	}
	if ss != nil && ss.Diagnostics != nil {
		stream := ss.Diagnostics
		diagnostics["stream_elapsed_ms"] = max(0, stream.EndedAt.Sub(stream.StartedAt).Milliseconds())
		diagnostics["received_events"] = stream.ReceivedEvents
		diagnostics["upstream_status"] = stream.UpstreamStatus
		diagnostics["upstream_protocol"] = stream.UpstreamProtocol
		diagnostics["stream_idle_timeout_seconds"] = stream.IdleTimeoutSeconds
		diagnostics["client_write_timeout_seconds"] = stream.WriteTimeoutSeconds
		diagnostics["ping_enabled"] = stream.PingEnabled
		if stream.PingEnabled {
			diagnostics["ping_interval_seconds"] = stream.PingIntervalSeconds
		}
		if !stream.FirstDataAt.IsZero() {
			diagnostics["first_event_elapsed_ms"] = max(0, stream.FirstDataAt.Sub(stream.StartedAt).Milliseconds())
		}
		if !stream.LastReadAt.IsZero() {
			diagnostics["last_upstream_activity_ms"] = max(0, stream.EndedAt.Sub(stream.LastReadAt).Milliseconds())
		}
		if stream.UpstreamReadError != nil {
			diagnostics["upstream_read_error"] = relayDiagnosticText(info, stream.UpstreamReadError.Error())
		}
	}
	other.SetAdmin("request_diagnostics", diagnostics)
}

// Bound untrusted diagnostic text and redact both credential formats and the
// actual keys for this request. Bodies, cookies and arbitrary headers are never captured.
func relayDiagnosticText(info *relaycommon.RelayInfo, value string) string {
	if info.TokenKey != "" {
		value = strings.ReplaceAll(value, info.TokenKey, "[redacted]")
	}
	if info.ChannelMeta != nil && info.ApiKey != "" {
		value = strings.ReplaceAll(value, info.ApiKey, "[redacted]")
	}
	value = common.MaskSensitiveInfo(value)
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > 512 {
		return string(runes[:512]) + "…"
	}
	return value
}

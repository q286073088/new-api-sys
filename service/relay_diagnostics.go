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
	if !streamFailed && !upstreamFailed && clientErr == nil && !info.MissingBillableUsage {
		return
	}

	diagnostics := map[string]any{
		"diagnostics_version":    3,
		"gateway_request_id":     info.RequestId,
		"gateway_version":        common.Version,
		"request_path":           ctx.Request.URL.Path,
		"recorded_at":            common.DiagnosticTime(time.Now()),
		"missing_billable_usage": info.MissingBillableUsage,
		"node_name":              common.NodeName,
		"client_protocol":        ctx.Request.Proto,
		"client_user_agent":      relayDiagnosticText(info, ctx.Request.UserAgent()),
		"attempt_number":         info.RetryIndex + 1,
		"request_host":           relayDiagnosticText(info, ctx.Request.Host),
	}
	if ray := ctx.GetHeader("CF-Ray"); ray != "" {
		diagnostics["cloudflare_ray"] = relayDiagnosticText(info, ray)
	}
	if !info.StartTime.IsZero() {
		diagnostics["request_started_at"] = common.DiagnosticTime(info.StartTime)
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
		diagnostics["downstream_headers_written"] = ctx.Writer.Written()
		diagnostics["downstream_written_bytes"] = max(0, ctx.Writer.Size())
	}
	if clientErr != nil {
		diagnostics["client_context_error"] = clientErr.Error()
		if cause := context.Cause(ctx.Request.Context()); cause != nil {
			diagnostics["client_context_cause"] = relayDiagnosticText(info, cause.Error())
		}
	}
	diagnosticStart := info.StartTime
	if received, _, _ := common.RequestTimelineSnapshot(ctx.Request.Context()); !received.IsZero() {
		diagnosticStart = received
	}
	if connection, ok := common.GetConnectionDiagnostics(ctx.Request.Context()); ok {
		for operation, failure := range map[string]common.ConnectionIOFailure{"read": connection.ReadFailure, "write": connection.WriteFailure} {
			if failure.At.IsZero() || failure.At.Before(diagnosticStart) {
				continue
			}
			prefix := "downstream_" + operation + "_"
			diagnostics[prefix+"error"] = relayDiagnosticText(info, failure.Error)
			diagnostics[prefix+"error_kind"] = failure.Kind
			diagnostics[prefix+"error_at"] = common.DiagnosticTime(failure.At)
			if !failure.Deadline.IsZero() {
				diagnostics[prefix+"deadline"] = common.DiagnosticTime(failure.Deadline)
			}
		}
		if !connection.ClosedAt.IsZero() && !connection.ClosedAt.Before(diagnosticStart) {
			diagnostics["gateway_connection_closed_at"] = common.DiagnosticTime(connection.ClosedAt)
		}
	}
	if deadline, ok := ctx.Request.Context().Deadline(); ok {
		diagnostics["client_deadline"] = common.DiagnosticTime(deadline)
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
		if !stream.ClientGoneAt.IsZero() {
			diagnostics["client_gone_at"] = common.DiagnosticTime(stream.ClientGoneAt)
		}
		if !stream.DrainStartedAt.IsZero() {
			diagnostics["upstream_drain_started_at"] = common.DiagnosticTime(stream.DrainStartedAt)
		}
		if !stream.DrainEndedAt.IsZero() {
			diagnostics["upstream_drain_ended_at"] = common.DiagnosticTime(stream.DrainEndedAt)
		}
		diagnostics["upstream_drain_timed_out"] = stream.DrainTimedOut
		diagnostics["drained_after_client_gone"] = stream.DrainedAfterClientGone
		diagnostics["stream_started_at"] = common.DiagnosticTime(stream.StartedAt)
		diagnostics["stream_ended_at"] = common.DiagnosticTime(stream.EndedAt)
		diagnostics["upstream_scanned_lines"] = stream.ScannedLines
		diagnostics["upstream_comment_lines"] = stream.CommentLines
		diagnostics["upstream_blank_lines"] = stream.BlankLines
		diagnostics["upstream_other_lines"] = stream.OtherLines
		events := make([]map[string]any, 0, len(stream.RecentEvents))
		for _, event := range stream.RecentEvents {
			events = append(events, map[string]any{"at": common.DiagnosticTime(event.At), "type": event.Type, "bytes": event.Bytes})
		}
		diagnostics["recent_upstream_events"] = events
		diagnostics["usage_event_seen"] = stream.UsageEventSeen
		diagnostics["terminal_event_seen"] = stream.TerminalEventSeen
		if !stream.UpstreamBodyClosedAt.IsZero() {
			diagnostics["upstream_body_closed_at"] = common.DiagnosticTime(stream.UpstreamBodyClosedAt)
		}
		if !stream.ScannerErrorAt.IsZero() {
			diagnostics["scanner_error_at"] = common.DiagnosticTime(stream.ScannerErrorAt)
			diagnostics["scanner_error_after_cleanup"] = stream.ScannerErrorAfterCleanup
		}
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
			diagnostics["last_upstream_data_at"] = common.DiagnosticTime(stream.LastReadAt)
			diagnostics["last_upstream_activity_ms"] = max(0, stream.EndedAt.Sub(stream.LastReadAt).Milliseconds())
		}
		if stream.UpstreamReadError != nil {
			diagnostics["upstream_read_error"] = relayDiagnosticText(info, stream.UpstreamReadError.Error())
		}
	}

	diagnostics["timezone"] = "Asia/Shanghai (UTC+08:00)"
	if started, phases, truncated := common.RequestTimelineSnapshot(ctx.Request.Context()); !started.IsZero() {
		diagnostics["gateway_received_at"] = common.DiagnosticTime(started)
		diagnostics["gateway_elapsed_ms"] = max(0, time.Since(started).Milliseconds())
		diagnostics["before_relay_elapsed_ms"] = max(0, info.StartTime.Sub(started).Milliseconds())
		diagnostics["request_phases"] = phases
		diagnostics["request_phases_truncated"] = truncated
		diagnostics["downstream_keepalive"] = common.RequestKeepaliveSnapshot(ctx.Request.Context())
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

package helper

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"

	"github.com/gin-gonic/gin"
)

const (
	InitialScannerBufferSize    = 64 << 10  // 64KB (64*1024)
	DefaultMaxScannerBufferSize = 128 << 20 // 64MB (64*1024*1024) default SSE buffer size
	DefaultPingInterval         = 10 * time.Second
	// clientGoneDrainTimeout is defined below so tests can use a short drain window.
	// streamWriteTimeout bounds a single blocked write to a slow client so the
	// unconditional wg.Wait() in cleanup can always finish. Without it, a slow
	// but connected client (full TCP buffer, no server WriteTimeout) could hang
	// the handler forever.
	streamWriteTimeout = 30 * time.Second
)

var clientGoneDrainTimeout = 120 * time.Second

func getScannerBufferSize() int {
	if constant.StreamScannerMaxBufferMB > 0 {
		return constant.StreamScannerMaxBufferMB << 20
	}
	return DefaultMaxScannerBufferSize
}

func NewStreamScanner(reader io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, InitialScannerBufferSize), getScannerBufferSize())
	return scanner
}

func copyCodexSSEHeaders(c *gin.Context, resp *http.Response) {
	if c == nil || c.Writer == nil || resp == nil {
		return
	}
	// codex
	for _, name := range []string{"X-Reasoning-Included", "X-Codex-Turn-State"} {
		values := resp.Header.Values(name)
		if !service.ShouldCopyUpstreamHeader(c, name, values) {
			continue
		}
		for _, value := range values {
			if value != "" {
				c.Writer.Header().Add(name, value)
			}
		}
	}
}

// ExtendWriteDeadline bounds a stream write. Callers must defer
// ClearWriteDeadline while holding their write lock: an armed HTTP/2 write
// deadline also resets the stream while it is idle waiting for upstream data.
func ExtendWriteDeadline(c *gin.Context) {
	if c == nil || c.Writer == nil {
		return
	}
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(streamWriteTimeout))
}

func ClearWriteDeadline(c *gin.Context) {
	if c == nil || c.Writer == nil {
		return
	}
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{})
}

func StreamScannerHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler func(data string, sr *StreamResult)) {

	if resp == nil || dataHandler == nil {
		return
	}

	// 无条件新建 StreamStatus
	info.StreamStatus = relaycommon.NewStreamStatus()
	streamStartedAt := time.Now()
	var firstDataAt, lastReadAt time.Time
	var upstreamReadError error
	var recentEvents []relaycommon.StreamEventDiagnostic
	var scannedLines, commentLines, blankLines, otherLines int
	var usageEventSeen, terminalEventSeen, scannerErrorAfterCleanup bool
	var upstreamBodyClosedAt, scannerErrorAt time.Time
	var clientGoneAt, drainStartedAt, drainEndedAt time.Time
	var drainTimedOut, drainedAfterClientGone bool
	receivedBeforeStream := info.ReceivedResponseCount

	ctx, cancel := context.WithCancel(context.Background())

	streamingTimeout := time.Duration(constant.StreamingTimeout) * time.Second

	var (
		stopChan    = make(chan bool, 3) // 增加缓冲区避免阻塞
		scanner     = NewStreamScanner(resp.Body)
		ticker      = time.NewTicker(streamingTimeout)
		pingTicker  *time.Ticker
		drainTimer  *time.Timer
		writeMutex  sync.Mutex     // Mutex to protect concurrent writes
		wg          sync.WaitGroup // 用于等待所有 goroutine 退出
		cleanupOnce sync.Once
		stopOnce    sync.Once
	)

	stop := func() {
		stopOnce.Do(func() {
			close(stopChan)
		})
	}

	generalSettings := operation_setting.GetGeneralSetting()
	pingEnabled := generalSettings.PingIntervalEnabled && !info.DisablePing
	pingInterval := time.Duration(generalSettings.PingIntervalSeconds) * time.Second
	if pingInterval <= 0 {
		pingInterval = DefaultPingInterval
	}

	if pingEnabled {
		pingTicker = time.NewTicker(pingInterval)
	}

	logger.LogDebug(c, "relay timeout seconds: %d", common.RelayTimeout)
	logger.LogDebug(c, "relay max idle conns: %d", common.RelayMaxIdleConns)
	logger.LogDebug(c, "relay max idle conns per host: %d", common.RelayMaxIdleConnsPerHost)
	logger.LogDebug(c, "streaming timeout seconds: %d", int64(streamingTimeout.Seconds()))
	logger.LogDebug(c, "ping interval seconds: %d", int64(pingInterval.Seconds()))

	cleanup := func() {
		cleanupOnce.Do(func() {
			cancel()
			stop()
			if resp.Body != nil {
				upstreamBodyClosedAt = time.Now()
				_ = resp.Body.Close()
			}

			ticker.Stop()
			if pingTicker != nil {
				pingTicker.Stop()
			}
			if drainTimer != nil {
				drainTimer.Stop()
			}

			wg.Wait()
		})
	}
	// Ensure gin.Context is not returned to Gin's pool while any stream goroutine can still use it.
	defer cleanup()

	scanner.Split(bufio.ScanLines)
	copyCodexSSEHeaders(c, resp)
	SetEventStreamHeaders(c)

	ctx = context.WithValue(ctx, "stop_chan", stopChan)

	// Handle ping data sending with improved error handling
	if pingEnabled && pingTicker != nil {
		wg.Add(1)
		gopool.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					logger.LogError(c, fmt.Sprintf("ping goroutine panic: %v", r))
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("ping panic: %v", r))
					stop()
				}
				logger.LogDebug(c, "ping goroutine exited")
				wg.Done()
			}()

			// 添加超时保护，防止 goroutine 无限运行
			maxPingDuration := 30 * time.Minute // 最大 ping 持续时间
			pingTimeout := time.NewTimer(maxPingDuration)
			defer pingTimeout.Stop()

			for {
				select {
				case <-pingTicker.C:
					var err error
					func() {
						writeMutex.Lock()
						defer writeMutex.Unlock()
						ExtendWriteDeadline(c)
						defer ClearWriteDeadline(c)
						err = PingData(c)
					}()
					if err != nil {
						logger.LogError(c, "ping data error: "+err.Error())
						info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPingFail, err)
						return
					}
					logger.LogDebug(c, "ping data sent")
				case <-ctx.Done():
					return
				case <-stopChan:
					return
				case <-c.Request.Context().Done():
					// 监听客户端断开连接
					return
				case <-pingTimeout.C:
					logger.LogError(c, "ping goroutine max duration reached")
					return
				}
			}
		})
	}

	dataChan := make(chan string, 10)

	wg.Add(1)
	gopool.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				logger.LogError(c, fmt.Sprintf("data handler goroutine panic: %v", r))
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("handler panic: %v", r))
			}
			stop()
			wg.Done()
		}()
		sr := newStreamResult(info.StreamStatus)
		for data := range dataChan {
			sr.reset()
			func() {
				writeMutex.Lock()
				defer writeMutex.Unlock()
				ExtendWriteDeadline(c)
				defer ClearWriteDeadline(c)
				dataHandler(data, sr)
			}()
			if sr.IsStopped() {
				return
			}
		}
	})

	// Scanner goroutine with improved error handling
	wg.Add(1)
	common.RelayCtxGo(ctx, func() {
		defer func() {
			close(dataChan)
			if r := recover(); r != nil {
				logger.LogError(c, fmt.Sprintf("scanner goroutine panic: %v", r))
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("scanner panic: %v", r))
			}
			stop()
			logger.LogDebug(c, "scanner goroutine exited")
			wg.Done()
		}()

		for scanner.Scan() {
			// 检查是否需要停止
			select {
			case <-stopChan:
				return
			case <-ctx.Done():
				return
			default:
			}

			ticker.Reset(streamingTimeout)
			lastReadAt = time.Now()
			data := scanner.Text()
			scannedLines++
			switch {
			case strings.TrimSpace(data) == "":
				blankLines++
			case strings.HasPrefix(data, ":"):
				commentLines++
			case !strings.HasPrefix(data, "data:") && !strings.HasPrefix(data, "[DONE]"):
				otherLines++
			}
			logger.LogDebug(c, "stream scanner data: %s", data)

			if len(data) < 6 {
				continue
			}
			if data[:5] != "data:" && data[:6] != "[DONE]" {
				continue
			}
			data = data[5:]
			data = strings.TrimSpace(data)
			if data == "" {
				continue
			}
			// Keep bounded structural metadata only, never model output or request content.
			var event struct {
				Type          string          `json:"type"`
				Usage         json.RawMessage `json:"usage"`
				UsageMetadata json.RawMessage `json:"usageMetadata"`
				Response      struct {
					Usage json.RawMessage `json:"usage"`
				} `json:"response"`
				Message struct {
					Usage json.RawMessage `json:"usage"`
				} `json:"message"`
			}
			eventType := "data"
			if common.Unmarshal([]byte(data), &event) == nil {
				switch event.Type {
				case "response.created", "response.in_progress", "response.output_item.added", "response.output_item.done", "response.content_part.added", "response.content_part.done", "response.output_text.delta", "response.output_text.done", "response.reasoning_summary_text.delta", "response.reasoning_summary_text.done", "message_start", "message_delta", "content_block_start", "content_block_delta", "content_block_stop", "ping", "error":
					eventType = event.Type
				case "response.completed", "response.incomplete", "response.failed", "message_stop":
					eventType = event.Type
					terminalEventSeen = true
				}
				for _, usage := range []json.RawMessage{event.Usage, event.UsageMetadata, event.Response.Usage, event.Message.Usage} {
					if value := strings.TrimSpace(string(usage)); value != "" && value != "null" {
						usageEventSeen = true
					}
				}
			}
			if strings.HasPrefix(data, "[DONE]") {
				eventType = "[DONE]"
				terminalEventSeen = true
			}
			if len(recentEvents) == 8 {
				recentEvents = recentEvents[1:]
			}
			recentEvents = append(recentEvents, relaycommon.StreamEventDiagnostic{At: lastReadAt.UTC(), Type: eventType, Bytes: len(data)})
			if !strings.HasPrefix(data, "[DONE]") {
				if firstDataAt.IsZero() {
					firstDataAt = lastReadAt
				}
				info.SetFirstResponseTime()
				info.ReceivedResponseCount++

				select {
				case dataChan <- data:
				case <-ctx.Done():
					return
				case <-stopChan:
					return
				}
			} else {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				logger.LogDebug(c, "received [DONE], stopping scanner")
				return
			}
		}

		if err := scanner.Err(); err != nil {
			if err != io.EOF {
				scannerErrorAt = time.Now()
				scannerErrorAfterCleanup = ctx.Err() != nil
				// Closing the body during cleanup is a consequence of stopping,
				// not evidence of a separate upstream failure.
				if ctx.Err() == nil && c.Request.Context().Err() == nil {
					upstreamReadError = err
				}
				logger.LogError(c, "scanner error: "+err.Error())
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, err)
			}
		}
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonEOF, nil)
	})

	// 主循环等待完成或超时
	select {
	case <-ticker.C:
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)
	case <-stopChan:
		// EndReason already set by the goroutine that triggered stopChan
	case <-c.Request.Context().Done():
		// The peer is gone, but the upstream may already have generated billable
		// work. Keep reading for a bounded period so a final usage event can settle.
		clientGoneAt = time.Now()
		drainStartedAt = clientGoneAt
		drainTimer = time.NewTimer(clientGoneDrainTimeout)
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, c.Request.Context().Err())
		select {
		case <-stopChan:
		case <-drainTimer.C:
			drainTimedOut = true
			stop()
			if resp.Body != nil {
				upstreamBodyClosedAt = time.Now()
				_ = resp.Body.Close()
			}
		}
		drainEndedAt = time.Now()
		drainedAfterClientGone = true
	}

	streamEndedAt := time.Now()
	cleanup()
	info.StreamStatus.Diagnostics = &relaycommon.StreamDiagnostics{
		ClientGoneAt:             clientGoneAt,
		DrainStartedAt:           drainStartedAt,
		DrainEndedAt:             drainEndedAt,
		DrainTimedOut:            drainTimedOut,
		DrainedAfterClientGone:   drainedAfterClientGone,
		RecentEvents:             recentEvents,
		ScannedLines:             scannedLines,
		CommentLines:             commentLines,
		BlankLines:               blankLines,
		OtherLines:               otherLines,
		UsageEventSeen:           usageEventSeen,
		TerminalEventSeen:        terminalEventSeen,
		UpstreamBodyClosedAt:     upstreamBodyClosedAt,
		ScannerErrorAt:           scannerErrorAt,
		ScannerErrorAfterCleanup: scannerErrorAfterCleanup,
		StartedAt:                streamStartedAt,
		EndedAt:                  streamEndedAt,
		FirstDataAt:              firstDataAt,
		LastReadAt:               lastReadAt,
		ReceivedEvents:           info.ReceivedResponseCount - receivedBeforeStream,
		UpstreamStatus:           resp.StatusCode,
		UpstreamProtocol:         resp.Proto,
		UpstreamReadError:        upstreamReadError,
		IdleTimeoutSeconds:       int(streamingTimeout / time.Second),
		WriteTimeoutSeconds:      int(streamWriteTimeout / time.Second),
		PingEnabled:              pingEnabled,
		PingIntervalSeconds:      int(pingInterval / time.Second),
	}
	if info.StreamStatus.IsNormalEnd() && !info.StreamStatus.HasErrors() {
		logger.LogInfo(c, fmt.Sprintf("stream ended: %s", info.StreamStatus.Summary()))
	} else {
		logger.LogError(c, fmt.Sprintf("stream ended: %s, received=%d", info.StreamStatus.Summary(), info.ReceivedResponseCount))
	}
}

package perfmetrics

import (
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
)

func TestCaptureRelaySampleUsesFinalAttemptTiming(t *testing.T) {
	requestStart := time.Unix(1000, 0)
	for _, tc := range []struct {
		name           string
		stream         bool
		success        bool
		tokens         int64
		first          time.Time
		wantTtft       int64
		wantHasTtft    bool
		wantGeneration int64
	}{
		{"stream_success", true, true, 50, requestStart.Add(32 * time.Second), 2000, true, 5000},
		{"nonstream_success", false, true, 70, time.Time{}, 0, false, 7000},
		{"final_failure", false, false, 0, time.Time{}, 0, false, 7000},
		{"stale_first_response", true, false, 0, requestStart.Add(time.Second), 0, false, 7000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				StartTime: requestStart, FirstResponseTime: tc.first,
				OriginModelName: "requested-model", UsingGroup: "final-group", IsStream: tc.stream,
				PerformanceAttempt: &relaycommon.RelayPerformanceAttempt{
					StartedAt: requestStart.Add(30 * time.Second), FirstResponseTime: tc.first,
				},
			}
			sample := CaptureRelaySample(info, tc.success, tc.tokens, requestStart.Add(37*time.Second))
			assert.Equal(t, Sample{
				Model: "requested-model", Group: "final-group", Success: tc.success,
				LatencyMs: 7000, TtftMs: tc.wantTtft, HasTtft: tc.wantHasTtft,
				OutputTokens: tc.tokens, GenerationMs: tc.wantGeneration,
			}, sample)
			assert.Equal(t, requestStart, info.StartTime, "administrator request duration remains available")
		})
	}
}

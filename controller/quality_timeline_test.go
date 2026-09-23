package controller

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestQualityTimelineSlotUsesLatestResult(t *testing.T) {
	db, target := qualityFixture(t, false)
	test := model.QualityTest{Name: "latest-slot", QualityTarget: target, Prompt: "Q", ExpectedAnswer: "A", IntervalMinutes: 30, Public: true}
	require.NoError(t, db.Create(&test).Error)
	now := time.Now().Unix()
	bucketTime := now - now%3600 + 600
	for _, status := range []string{"failed", "passed", "pending"} {
		result := model.QualityResult{
			TestID: test.ID, Name: test.Name, Model: test.Model, Group: test.Group,
			Prompt: test.Prompt, ExpectedAnswer: test.ExpectedAnswer, Status: status,
			StartedAt: bucketTime, FinishedAt: now,
		}
		require.NoError(t, db.Create(&result).Error)
		time.Sleep(10 * time.Millisecond)
	}
	rec := qualityAPI(t, QualitySummary, "/summary", false, nil, "")
	require.Equal(t, http.StatusOK, rec.Code)
	slots := gjson.GetBytes(rec.Body.Bytes(), fmt.Sprintf("data.timeline.%d", test.ID)).Array()
	require.Len(t, slots, 48)
	current := slots[len(slots)-1]
	assert.Equal(t, "pending", current.Get("status").String())
	assert.Equal(t, int64(3), current.Get("count").Int())
}

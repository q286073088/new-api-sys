package controller

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestHTTPRelayRespectsModelRetryLimitsAndFinalLog(t *testing.T) {
	perfSetting := config.GlobalConfig.Get("perf_metrics_setting").(*perf_metrics_setting.PerfMetricsSetting)
	previousPerfSetting := *perfSetting
	perfSetting.Enabled = true
	t.Cleanup(func() { *perfSetting = previousPerfSetting })
	oldConfig, oldPrices := operation_setting.ModelRetryTimesJSON(), ratio_setting.ModelPrice2JSONString()
	oldUsableGroups, oldGroupRatios := setting.UserUsableGroups2JSONString(), ratio_setting.GroupRatio2JSONString()
	oldRetry, oldCount, oldErrorLog, oldConsume := common.RetryTimes, constant.CountToken, constant.ErrorLogEnabled, common.LogConsumeEnabled
	oldMemory, oldDisable, oldExport := common.MemoryCacheEnabled, common.AutomaticDisableChannelEnabled, common.DataExportEnabled
	oldFree := operation_setting.GetQuotaSetting().EnableFreeModelPreConsume
	t.Cleanup(func() {
		require.NoError(t, operation_setting.UpdateModelRetryTimes(oldConfig))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(oldUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroupRatios))
		common.RetryTimes, constant.CountToken, constant.ErrorLogEnabled, common.LogConsumeEnabled = oldRetry, oldCount, oldErrorLog, oldConsume
		common.MemoryCacheEnabled, common.AutomaticDisableChannelEnabled, common.DataExportEnabled = oldMemory, oldDisable, oldExport
		operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = oldFree
	})
	common.RetryTimes, constant.CountToken, constant.ErrorLogEnabled, common.LogConsumeEnabled = 1, false, true, true
	common.MemoryCacheEnabled, common.AutomaticDisableChannelEnabled, common.DataExportEnabled = false, false, false
	operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = false
	require.NoError(t, operation_setting.UpdateModelRetryTimes(`{"no-retry":0,"two-retries":2,"three-retries":3}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"no-retry":0,"two-retries":0,"three-retries":0,"global-default":0}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","backup":"Backup"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"backup":1}`))
	for _, tc := range []struct {
		name, model string
		attempts    int
		succeed     bool
		crossGroup  bool
		lastChannel int
	}{
		{"disabled", "no-retry", 1, false, false, 1},
		{"two_retries", "two-retries", 3, false, false, 3},
		{"three_retries", "three-retries", 4, false, false, 4},
		{"global_fallback", "global-default", 2, false, false, 2},
		{"success_after_retry", "two-retries", 3, true, false, 3},
		{"success_second_attempt", "global-default", 2, true, false, 2},
		{"global_fallback_preserves_cross_group_retries", "global-default", 4, true, true, 4},
		{"model_override_bounds_cross_group_retries", "two-retries", 3, false, true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modelName := tc.model + "-" + tc.name
			retryLimits := map[string]int{"no-retry": 0, "two-retries": 2, "three-retries": 3}
			if limit, configured := retryLimits[tc.model]; configured {
				retryLimits[modelName] = limit
			}
			require.NoError(t, operation_setting.UpdateModelRetryTimes(common.GetJsonString(retryLimits)))
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(common.GetJsonString(map[string]int{modelName: 0})))
			db := setupManageUserTestDB(t)
			t.Setenv("LOG_SQL_DSN", "")
			logDB := model.LOG_DB
			require.NoError(t, model.InitLogDB()) // Initialize dialect-specific column quoting through startup.
			model.LOG_DB = logDB
			require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.PerfMetric{}))
			user := model.User{Username: "retry-owner", AffCode: "retry", Group: "default"}
			require.NoError(t, db.Create(&user).Error)
			var attempts atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempt := attempts.Add(1)
				var body struct{ Model string }
				require.NoError(t, common.DecodeJson(r.Body, &body))
				assert.Equal(t, "provider-model", body.Model, "retry overrides use the client model before channel mapping")
				w.Header().Set("Content-Type", "application/json")
				if tc.succeed && int(attempt) == tc.attempts {
					_, _ = io.WriteString(w, `{"id":"completion","object":"chat.completion","model":"provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
					return
				}
				w.WriteHeader(http.StatusBadGateway)
				_, _ = io.WriteString(w, `{"error":{"message":"temporary provider failure","type":"server_error","code":"server_error"}}`)
			}))
			t.Cleanup(upstream.Close)
			var first model.Channel
			for i := range 4 {
				priority := int64(4 - i)
				group := "default"
				if tc.crossGroup && i >= 2 {
					group = "backup"
				}
				channel := model.Channel{Name: fmt.Sprintf("retry-channel-%d", i), Type: constant.ChannelTypeOpenAI, Key: "test-key", Models: modelName, Group: group, Status: common.ChannelStatusEnabled,
					BaseURL: common.GetPointer(upstream.URL), Priority: &priority, ModelMapping: common.GetPointer(common.GetJsonString(map[string]string{modelName: "provider-model"}))}
				require.NoError(t, db.Create(&channel).Error)
				require.NoError(t, db.Create(&model.Ability{ChannelId: channel.Id, Group: group, Model: modelName, Enabled: true, Priority: &priority}).Error)
				if i == 0 {
					first = channel
				}
			}
			w := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(w)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(common.GetJsonString(map[string]any{
				"model": modelName, "messages": []map[string]string{{"role": "user", "content": "hello"}},
			})))
			ctx.Request.Header.Set("Content-Type", "application/json")
			ctx.Set("id", user.Id)
			ctx.Set("username", user.Username)
			ctx.Set("token_id", 11)
			common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
			if tc.crossGroup {
				common.SetContextKey(ctx, constant.ContextKeyTokenGroup, "auto")
				common.SetContextKey(ctx, constant.ContextKeyAutoGroup, "default")
				common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, true)
				common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"default", "backup"})
			}
			ctx.Set(common.RequestIdKey, "http-retry-request")
			require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, &first, modelName))
			Relay(ctx, types.RelayFormatOpenAI)
			assert.EqualValues(t, tc.attempts, attempts.Load(), w.Body.String())
			wantType, wantStatus := model.LogTypeError, http.StatusBadGateway
			if tc.succeed {
				wantType, wantStatus = model.LogTypeConsume, http.StatusOK
			}
			assert.Equal(t, wantStatus, w.Code)
			logs, count, err := model.GetUserLogs(user.Id, model.LogTypeUnknown, 0, 0, "", "", 0, 10, "", "http-retry-request", "")
			require.NoError(t, err)
			assert.EqualValues(t, 1, count)
			require.Len(t, logs, 1)
			assert.Equal(t, wantType, logs[0].Type)
			assert.Equal(t, first.Id+tc.lastChannel-1, logs[0].ChannelId)
			_, count, err = model.GetAllLogs(model.LogTypeUnknown, 0, 0, "", "", "", 0, 10, 0, "", "http-retry-request", "")
			require.NoError(t, err)
			assert.EqualValues(t, tc.attempts, count)
			var metric perfmetrics.ModelSummary
			require.EventuallyWithT(t, func(t *assert.CollectT) {
				summary, err := perfmetrics.QuerySummaryAll(1, nil)
				if !assert.NoError(t, err) {
					return
				}
				for _, candidate := range summary.Models {
					if candidate.ModelName == modelName {
						metric = candidate
						return
					}
				}
				assert.Fail(t, "waiting for the final performance sample")
			}, 3*time.Second, 10*time.Millisecond)
			assert.EqualValues(t, 1, metric.RequestCount, "intermediate channel attempts must not affect model-square status")
			wantSuccessRate := float64(0)
			if tc.succeed {
				wantSuccessRate = 100
			}
			assert.Equal(t, wantSuccessRate, metric.SuccessRate)
			performance, err := perfmetrics.Query(perfmetrics.QueryParams{Model: modelName, Hours: 1})
			require.NoError(t, err)
			require.Len(t, performance.Groups, 1, "only the final channel's group receives a sample")
			wantGroup := "default"
			if tc.crossGroup && tc.lastChannel >= 3 {
				wantGroup = "backup"
			}
			assert.Equal(t, wantGroup, performance.Groups[0].Group)
		})
	}
}

func TestRelayRetryLogsExposeOnlyFinalOutcomeToUsers(t *testing.T) {
	for _, success := range []bool{false, true} {
		t.Run(fmt.Sprintf("success_%t", success), func(t *testing.T) {
			db := setupManageUserTestDB(t)
			require.NoError(t, db.AutoMigrate(&model.Channel{}))
			oldErrors, oldConsume := constant.ErrorLogEnabled, common.LogConsumeEnabled
			constant.ErrorLogEnabled, common.LogConsumeEnabled = true, true
			t.Cleanup(func() { constant.ErrorLogEnabled, common.LogConsumeEnabled = oldErrors, oldConsume })
			require.NoError(t, db.Create(&model.User{Id: 7, Username: "retry-owner"}).Error)
			if !success {
				require.NoError(t, model.LOG_DB.Migrator().DropColumn(&model.Log{}, "HiddenForUser"))
				require.NoError(t, model.LOG_DB.Omit("HiddenForUser").Create(&model.Log{
					UserId: 7, Type: model.LogTypeError, Content: "historical failure", RequestId: "historical-request",
				}).Error)
			}
			for range 2 {
				require.NoError(t, model.LOG_DB.AutoMigrate(&model.Log{}))
			}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			ctx.Set("id", 7)
			ctx.Set("token_id", 11)
			ctx.Set("original_model", "requested-model")
			ctx.Set(common.RequestIdKey, "retry-request")
			model.BeginRelayErrorLogs(ctx)
			for _, id := range []int{101, 102} {
				processChannelError(ctx, types.ChannelError{ChannelId: id}, types.NewOpenAIError(errors.New("upstream failure"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway), nil)
			}
			wantChannel, wantType := 102, model.LogTypeError
			if success {
				model.RecordConsumeLog(ctx, 7, model.RecordConsumeLogParams{ChannelId: 103, ModelName: "requested-model", TokenId: 11, Quota: 10, Other: model.NewLogOther()})
				wantChannel, wantType = 103, model.LogTypeConsume
			}
			model.FlushRelayErrorLogs(ctx, success)
			model.FlushRelayErrorLogs(ctx, success)
			for range 2 {
				require.NoError(t, model.LOG_DB.AutoMigrate(&model.Log{}))
			}
			if !success {
				historical, count, err := model.GetUserLogs(7, model.LogTypeUnknown, 0, 0, "", "", 0, 10, "", "historical-request", "")
				require.NoError(t, err)
				assert.EqualValues(t, 1, count)
				require.Len(t, historical, 1)
				assert.Equal(t, "historical failure", historical[0].Content)
			}
			for _, logType := range []int{model.LogTypeUnknown, model.LogTypeError} {
				logs, count, err := model.GetUserLogs(7, logType, 0, 0, "", "", 0, 1, "", "retry-request", "")
				require.NoError(t, err)
				if success && logType == model.LogTypeError {
					assert.Zero(t, count)
					assert.Empty(t, logs)
					continue
				}
				assert.EqualValues(t, 1, count)
				require.Len(t, logs, 1)
				assert.Equal(t, wantChannel, logs[0].ChannelId)
				assert.Equal(t, wantType, logs[0].Type)
			}
			logs, err := model.GetLogByTokenId(11)
			require.NoError(t, err)
			require.Len(t, logs, 1)
			assert.Equal(t, wantChannel, logs[0].ChannelId)
			var stored []model.Log
			require.NoError(t, model.LOG_DB.Where("request_id = ?", "retry-request").Order("id").Find(&stored).Error)
			assert.Equal(t, 101, stored[0].ChannelId)
			assert.Equal(t, 102, stored[1].ChannelId)
			if success {
				assert.Len(t, stored, 3)
			} else {
				assert.Len(t, stored, 2)
			}
			adminLogs, count, err := model.GetAllLogs(model.LogTypeUnknown, 0, 0, "", "", "", 0, 10, 0, "", "retry-request", "")
			require.NoError(t, err)
			assert.Len(t, adminLogs, len(stored))
			assert.EqualValues(t, len(stored), count)
		})
	}
}

func TestProcessChannelErrorUsesSnapshotWithoutLeakingChannelMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	previousErrorLogEnabled := constant.ErrorLogEnabled

	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, database.AutoMigrate(&model.User{}, &model.Log{}))
	model.DB, model.LOG_DB = database, database
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		constant.ErrorLogEnabled = previousErrorLogEnabled
		require.NoError(t, sqlDB.Close())
	})

	require.NoError(t, database.Create(&model.User{Id: 7, Username: "log-owner", Group: "default"}).Error)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("id", 7)
	ctx.Set("username", "log-owner")
	ctx.Set("token_name", "test-token")
	ctx.Set("token_id", 11)
	ctx.Set("original_model", "gpt-test")
	ctx.Set("group", "default")
	ctx.Set("channel_id", 202)
	ctx.Set("channel_name", "mutable-context-channel")
	ctx.Set("channel_type", 9)
	ctx.Set("use_channel", []string{"101"})
	common.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now().Add(-time.Second))

	channelSnapshot := types.ChannelError{
		ChannelId:   101,
		ChannelType: 1,
		ChannelName: "snapshot-channel",
		AutoBan:     false,
	}
	apiErr := types.NewOpenAIError(errors.New("upstream failed"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)

	processChannelError(ctx, channelSnapshot, apiErr, nil)

	var stored model.Log
	require.NoError(t, database.First(&stored).Error)
	assert.Equal(t, channelSnapshot.ChannelId, stored.ChannelId)
	storedOther, err := common.StrToMap(stored.Other)
	require.NoError(t, err)
	assert.Equal(t, float64(http.StatusBadGateway), storedOther["status_code"])
	for _, key := range []string{"channel_id", "channel_name", "channel_type"} {
		assert.NotContains(t, storedOther, key)
	}
	adminInfo, ok := storedOther["admin_info"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{"101"}, adminInfo["use_channel"])

	logs, total, err := model.GetUserLogs(7, model.LogTypeError, 0, 0, "", "", 0, 10, "", "", "")
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, logs, 1)
	assert.Equal(t, channelSnapshot.ChannelId, logs[0].ChannelId)
	assert.Empty(t, logs[0].ChannelName)
	userOther, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	assert.NotContains(t, userOther, "admin_info")
	for _, key := range []string{"channel_id", "channel_name", "channel_type"} {
		assert.NotContains(t, userOther, key)
	}
}

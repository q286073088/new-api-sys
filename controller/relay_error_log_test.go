package controller

import (
	"context"
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
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/billing_setting"
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
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	perfSetting := config.GlobalConfig.Get("perf_metrics_setting").(*perf_metrics_setting.PerfMetricsSetting)
	previousPerfSetting := *perfSetting
	perfSetting.Enabled = true
	t.Cleanup(func() { *perfSetting = previousPerfSetting })
	billingSetting := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
	previousBillingSetting := *billingSetting
	t.Cleanup(func() { *billingSetting = previousBillingSetting })
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
		name, model     string
		attempts        int
		succeed         bool
		crossGroup      bool
		lastChannel     int
		upstreamBody    string
		stream          bool
		paid            bool
		format          types.RelayFormat
		channelType     int
		responseStarted bool
		cancelOnWrite   bool
		successBody     string
		expression      string
		freeGroup       bool
		expectedQuota   int
		expectedError   types.ErrorCode
	}{
		{name: "disabled", model: "no-retry", attempts: 1, lastChannel: 1},
		{name: "two_retries", model: "two-retries", attempts: 3, lastChannel: 3},
		{name: "three_retries", model: "three-retries", attempts: 4, lastChannel: 4},
		{name: "global_fallback", model: "global-default", attempts: 2, lastChannel: 2},
		{name: "success_after_retry", model: "two-retries", attempts: 3, succeed: true, lastChannel: 3},
		{name: "success_second_attempt", model: "global-default", attempts: 2, succeed: true, lastChannel: 2},
		{name: "global_fallback_preserves_cross_group_retries", model: "global-default", attempts: 4, succeed: true, crossGroup: true, lastChannel: 4},
		{name: "model_override_bounds_cross_group_retries", model: "two-retries", attempts: 3, crossGroup: true, lastChannel: 2},
		{name: "empty_stream_retries", model: "two-retries", attempts: 3, lastChannel: 3, stream: true, upstreamBody: "data: [DONE]\n\n"},
		{name: "empty_stream_refunds", model: "two-retries", attempts: 3, lastChannel: 3, stream: true, paid: true, upstreamBody: "data: [DONE]\n\n"},
		{name: "empty_stream_then_success", model: "two-retries", attempts: 2, lastChannel: 2, stream: true, paid: true, succeed: true, upstreamBody: "data: [DONE]\n\n"},
		{name: "empty_json_then_success", model: "two-retries", attempts: 2, lastChannel: 2, paid: true, succeed: true, upstreamBody: `{"choices":[],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`},
		{name: "non_stream_after_empty_sse", model: "two-retries", attempts: 2, lastChannel: 2, paid: true, succeed: true, upstreamBody: "data: [DONE]\n\n"},
		{name: "empty_stream_retry_disabled", model: "no-retry", attempts: 1, lastChannel: 1, stream: true, paid: true, upstreamBody: "data: [DONE]\n\n"},
		{name: "responses_empty_stream", model: "two-retries", attempts: 3, lastChannel: 3, format: types.RelayFormatOpenAIResponses, stream: true, paid: true, upstreamBody: "data: [DONE]\n\n"},
		{name: "responses_empty_completion_then_success", model: "two-retries", attempts: 2, lastChannel: 2, format: types.RelayFormatOpenAIResponses, stream: true, paid: true, succeed: true, upstreamBody: "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n"},
		{name: "responses_empty_json_then_success", model: "two-retries", attempts: 2, lastChannel: 2, format: types.RelayFormatOpenAIResponses, paid: true, succeed: true, upstreamBody: `{"object":"response","status":"completed","output":[]}`},
		{name: "claude_empty_stream", model: "two-retries", attempts: 3, lastChannel: 3, channelType: constant.ChannelTypeAnthropic, stream: true, paid: true, upstreamBody: "data: [DONE]\n\n"},
		{name: "claude_empty_json", model: "two-retries", attempts: 3, lastChannel: 3, channelType: constant.ChannelTypeAnthropic, paid: true, upstreamBody: `{"type":"message","content":[],"usage":{"input_tokens":0,"output_tokens":0}}`},
		{name: "gemini_empty_stream", model: "two-retries", attempts: 3, lastChannel: 3, channelType: constant.ChannelTypeGemini, stream: true, paid: true, upstreamBody: "data: [DONE]\n\n"},
		{name: "gemini_empty_json", model: "two-retries", attempts: 3, lastChannel: 3, channelType: constant.ChannelTypeGemini, paid: true, upstreamBody: `{"candidates":[],"usageMetadata":{}}`},
		{name: "started_stream_not_retried", model: "two-retries", attempts: 1, lastChannel: 1, stream: true, paid: true, responseStarted: true, upstreamBody: "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\ndata: {\"choices\":[],\"usage\":{}}\n\ndata: [DONE]\n\n"},
		{name: "canceled_stream_not_retried", model: "two-retries", attempts: 1, lastChannel: 1, stream: true, paid: true, responseStarted: true, cancelOnWrite: true, upstreamBody: "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\ndata: {\"choices\":[],\"usage\":{}}\n\ndata: [DONE]\n\n"},
		{name: "valid_completion_without_upstream_usage", model: "two-retries", attempts: 1, lastChannel: 1, paid: true, succeed: true, successBody: `{"id":"completion","model":"provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`},
		{name: "fixed_price_without_tokens", model: "two-retries", attempts: 1, lastChannel: 1, paid: true, succeed: true, expression: `tier("request", fixed(0.01))`, successBody: `{"choices":[],"usage":{}}`},
		{name: "free_fixed_price_without_tokens", model: "two-retries", attempts: 1, lastChannel: 1, succeed: true, expression: `tier("free", fixed(0))`, successBody: `{"choices":[],"usage":{}}`},
		{name: "tool_only_billing", model: "two-retries", attempts: 1, lastChannel: 1, format: types.RelayFormatOpenAIResponses, paid: true, succeed: true, expectedQuota: 10000, successBody: `{"status":"completed","output":[{"type":"web_search_call","id":"search_1","status":"completed"}]}`},
		{name: "free_group_tool_only", model: "two-retries", attempts: 1, lastChannel: 1, format: types.RelayFormatOpenAIResponses, freeGroup: true, succeed: true, successBody: `{"status":"completed","output":[{"type":"web_search_call","id":"search_1","status":"completed"}]}`},
		{name: "openai_rejection_not_retried", model: "two-retries", attempts: 1, lastChannel: 1, paid: true, expectedError: types.ErrorCodePromptBlocked, upstreamBody: `{"choices":[{"index":0,"message":{"role":"assistant","content":null},"finish_reason":"content_filter"}],"usage":{}}`},
		{name: "claude_rejection_not_retried", model: "two-retries", attempts: 1, lastChannel: 1, paid: true, channelType: constant.ChannelTypeAnthropic, expectedError: types.ErrorCodePromptBlocked, upstreamBody: `{"type":"message","content":[],"stop_reason":"refusal","usage":{}}`},
		{name: "gemini_rejection_not_retried", model: "two-retries", attempts: 1, lastChannel: 1, paid: true, channelType: constant.ChannelTypeGemini, expectedError: types.ErrorCodePromptBlocked, upstreamBody: `{"candidates":[],"promptFeedback":{"blockReason":"SAFETY"},"usageMetadata":{}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modelName := tc.model + "-" + tc.name
			retryLimits := map[string]int{"no-retry": 0, "two-retries": 2, "three-retries": 3}
			if limit, configured := retryLimits[tc.model]; configured {
				retryLimits[modelName] = limit
			}
			require.NoError(t, operation_setting.UpdateModelRetryTimes(common.GetJsonString(retryLimits)))
			price := float64(0)
			if tc.paid {
				price = 0.01
			}
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(common.GetJsonString(map[string]float64{modelName: price})))
			billingSetting.BillingMode, billingSetting.BillingExpr = map[string]string{}, map[string]string{}
			if tc.expression != "" {
				billingSetting.BillingMode[modelName] = billing_setting.BillingModeTieredExpr
				billingSetting.BillingExpr[modelName] = tc.expression
			}
			groupRatio := 1
			if tc.freeGroup {
				groupRatio = 0
			}
			require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(common.GetJsonString(map[string]int{"default": groupRatio, "backup": 1})))
			db := setupManageUserTestDB(t)
			t.Setenv("LOG_SQL_DSN", "")
			logDB := model.LOG_DB
			require.NoError(t, model.InitLogDB()) // Initialize dialect-specific column quoting through startup.
			model.LOG_DB = logDB
			require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.PerfMetric{}, &model.Token{}))
			const startingQuota = 100000
			user := model.User{Username: "retry-owner", AffCode: "retry", Group: "default", Quota: startingQuota}
			require.NoError(t, db.Create(&user).Error)
			token := model.Token{UserId: user.Id, Key: "missing-usage-test-token", Status: common.TokenStatusEnabled, RemainQuota: startingQuota}
			require.NoError(t, db.Create(&token).Error)
			format := tc.format
			if format == "" {
				format = types.RelayFormatOpenAI
			}
			channelType := tc.channelType
			if channelType == 0 {
				channelType = constant.ChannelTypeOpenAI
			}
			var attempts atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempt := attempts.Add(1)
				var body struct{ Model string }
				require.NoError(t, common.DecodeJson(r.Body, &body))
				if channelType == constant.ChannelTypeGemini {
					assert.Contains(t, r.URL.Path, "provider-model")
				} else {
					assert.Equal(t, "provider-model", body.Model, "retry overrides use the client model before channel mapping")
				}
				if tc.paid {
					var held model.User
					require.NoError(t, db.First(&held, user.Id).Error)
					assert.Equal(t, startingQuota-5000, held.Quota, "one reservation must remain open throughout retries")
				}
				w.Header().Set("Content-Type", "application/json")
				if tc.succeed && int(attempt) == tc.attempts {
					response := tc.successBody
					if response == "" {
						response = `{"id":"completion","object":"chat.completion","model":"provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
						if format == types.RelayFormatOpenAIResponses {
							response = `{"id":"completion","object":"response","model":"provider-model","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
							if tc.stream {
								response = "data: {\"type\":\"response.completed\",\"response\":" + response + "}\n\n"
							}
						} else if tc.stream {
							response = "data: {\"id\":\"completion\",\"model\":\"provider-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\ndata: [DONE]\n\n"
						}
					}
					if tc.stream {
						w.Header().Set("Content-Type", "text/event-stream")
					}
					_, _ = io.WriteString(w, response)
					return
				}
				if tc.upstreamBody != "" {
					if tc.stream || strings.HasPrefix(tc.upstreamBody, "data:") {
						w.Header().Set("Content-Type", "text/event-stream")
					}
					_, _ = io.WriteString(w, tc.upstreamBody)
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
				channel := model.Channel{Name: fmt.Sprintf("retry-channel-%d", i), Type: channelType, Key: "test-key", Models: modelName, Group: group, Status: common.ChannelStatusEnabled,
					BaseURL: common.GetPointer(upstream.URL), Priority: &priority, ModelMapping: common.GetPointer(common.GetJsonString(map[string]string{modelName: "provider-model"}))}
				require.NoError(t, db.Create(&channel).Error)
				require.NoError(t, db.Create(&model.Ability{ChannelId: channel.Id, Group: group, Model: modelName, Enabled: true, Priority: &priority}).Error)
				if i == 0 {
					first = channel
				}
			}
			w := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(w)
			path := "/v1/chat/completions"
			requestBody := map[string]any{"model": modelName, "messages": []map[string]string{{"role": "user", "content": "hello"}}, "stream": tc.stream}
			if format == types.RelayFormatOpenAIResponses {
				path = "/v1/responses"
				requestBody = map[string]any{"model": modelName, "input": "hello", "stream": tc.stream}
			}
			ctx.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(common.GetJsonString(requestBody)))
			if tc.cancelOnWrite {
				clientContext, cancel := context.WithCancel(ctx.Request.Context())
				t.Cleanup(cancel)
				ctx.Request = ctx.Request.WithContext(clientContext)
				ctx.Writer = &cancelRelayResponseWriter{ResponseWriter: ctx.Writer, cancel: cancel}
			}
			ctx.Request.Header.Set("Content-Type", "application/json")
			ctx.Set("id", user.Id)
			ctx.Set("username", user.Username)
			ctx.Set("token_id", token.Id)
			common.SetContextKey(ctx, constant.ContextKeyTokenKey, token.Key)
			common.SetContextKey(ctx, constant.ContextKeyUserSetting, dto.UserSetting{BillingPreference: "wallet_only"})
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
			Relay(ctx, format)
			if tc.cancelOnWrite {
				require.ErrorIs(t, ctx.Request.Context().Err(), context.Canceled)
			}
			assert.EqualValues(t, tc.attempts, attempts.Load(), w.Body.String())
			wantType, wantStatus := model.LogTypeError, http.StatusBadGateway
			wantError := types.ErrorCodeMissingUsage
			if tc.expectedError != "" {
				wantError = tc.expectedError
				wantStatus = http.StatusBadRequest
			}
			if tc.succeed {
				wantType, wantStatus = model.LogTypeConsume, http.StatusOK
			}
			if tc.responseStarted {
				wantStatus = http.StatusOK
			}
			assert.Equal(t, wantStatus, w.Code)
			wantContentType := "application/json"
			if tc.stream && (tc.succeed || tc.responseStarted) {
				wantContentType = "text/event-stream"
			}
			assert.Contains(t, w.Header().Get("Content-Type"), wantContentType)
			if tc.succeed {
				assert.NotContains(t, w.Body.String(), "missing_usage", "failed attempts must not be sent to the client")
				if tc.stream && format == types.RelayFormatOpenAI {
					assert.Equal(t, 1, strings.Count(w.Body.String(), "[DONE]"))
				}
			} else if !tc.cancelOnWrite {
				if tc.upstreamBody != "" {
					assert.Contains(t, w.Body.String(), string(wantError))
					assert.NotContains(t, w.Body.String(), "[DONE]")
				}
			}
			logs, count, err := model.GetUserLogs(user.Id, model.LogTypeUnknown, 0, 0, "", "", 0, 10, "", "http-retry-request", "")
			require.NoError(t, err)
			assert.EqualValues(t, 1, count)
			require.Len(t, logs, 1)
			assert.Equal(t, wantType, logs[0].Type)
			assert.Equal(t, first.Id+tc.lastChannel-1, logs[0].ChannelId)
			adminLogs, count, err := model.GetAllLogs(model.LogTypeUnknown, 0, 0, "", "", "", 0, 10, 0, "", "http-retry-request", "")
			require.NoError(t, err)
			assert.EqualValues(t, tc.attempts, count)
			for _, entry := range adminLogs {
				assert.NotContains(t, entry.Other, "channel_test", "ordinary requests must not record model answers")
			}
			if tc.upstreamBody != "" {
				for _, entry := range adminLogs {
					if entry.Type != model.LogTypeError {
						continue
					}
					other, err := common.StrToMap(entry.Other)
					require.NoError(t, err)
					status := http.StatusBadGateway
					if tc.expectedError != "" {
						status = http.StatusBadRequest
					}
					assert.Equal(t, float64(status), other["status_code"])
					assert.Equal(t, string(wantError), other["error_code"])
				}
			}
			wantQuota := 0
			if tc.paid && tc.succeed {
				wantQuota = 5000
			}
			if tc.expectedQuota != 0 {
				wantQuota = tc.expectedQuota
			}
			require.EventuallyWithT(t, func(t *assert.CollectT) {
				var storedUser model.User
				var storedToken model.Token
				if !assert.NoError(t, db.First(&storedUser, user.Id).Error) || !assert.NoError(t, db.First(&storedToken, token.Id).Error) {
					return
				}
				assert.Equal(t, startingQuota-wantQuota, storedUser.Quota)
				assert.Equal(t, wantQuota, storedUser.UsedQuota)
				assert.Equal(t, startingQuota-wantQuota, storedToken.RemainQuota)
				assert.Equal(t, wantQuota, storedToken.UsedQuota)
			}, 3*time.Second, 10*time.Millisecond)
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

type cancelRelayResponseWriter struct {
	gin.ResponseWriter
	cancel context.CancelFunc
}

func (w *cancelRelayResponseWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	w.cancel()
	return n, err
}

func TestMissingUsageRetryHonorsConnectionState(t *testing.T) {
	for _, tc := range []struct {
		name              string
		canceled, written bool
		wantRetry         bool
	}{
		{name: "uncommitted", wantRetry: true},
		{name: "canceled_before_response", canceled: true},
		{name: "response_already_started", written: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			requestContext, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(requestContext)
			if tc.canceled {
				cancel()
			}
			if tc.written {
				_, err := c.Writer.Write([]byte("data: response\n\n"))
				require.NoError(t, err)
			}
			err := types.NewOpenAIError(errors.New("missing usage"), types.ErrorCodeMissingUsage, http.StatusBadGateway)
			assert.Equal(t, tc.wantRetry, shouldRetry(c, err, 2))
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

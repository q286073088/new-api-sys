package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

func setupChannelProbeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupManageUserTestDB(t)
	t.Setenv("LOG_SQL_DSN", "")
	logDB := model.LOG_DB
	require.NoError(t, model.InitLogDB()) // Initialize quoting for the tested dialect.
	model.LOG_DB = logDB
	return db
}

func TestChannelTestSettingsPersistence(t *testing.T) {
	db := setupChannelProbeTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	monitor := operation_setting.GetMonitorSetting()
	previous, previousOptions := *monitor, common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { *monitor, common.OptionMap = previous, previousOptions })
	const prompt = "请计算 17×23，说明\"理由\"。\n保留换行 🧪"
	require.NoError(t, model.UpdateOption(operation_setting.ChannelTestPromptOptionKey, prompt))
	require.NoError(t, model.UpdateOption(operation_setting.ChannelTestMaxTokensOptionKey, "2048"))
	assert.Equal(t, prompt, monitor.TestPrompt())
	assert.Equal(t, uint(2048), monitor.TestMaxTokens())
	for _, value := range []string{"0", "-1", "32769", "1.5", "invalid"} {
		assert.Error(t, model.UpdateOption(operation_setting.ChannelTestMaxTokensOptionKey, value))
	}
	assert.Error(t, model.UpdateOption(operation_setting.ChannelTestPromptOptionKey, strings.Repeat("题", 20001)))
	var saved []model.Option
	require.NoError(t, db.Find(&saved).Error)
	persisted := make(map[string]string)
	for _, option := range saved {
		persisted[option.Key] = option.Value
	}
	assert.Equal(t, prompt, persisted[operation_setting.ChannelTestPromptOptionKey])
	assert.Equal(t, "2048", persisted[operation_setting.ChannelTestMaxTokensOptionKey])
	monitor.ChannelTestPrompt, monitor.ChannelTestMaxTokens = "", 0
	require.NoError(t, config.GlobalConfig.LoadFromDB(persisted))
	assert.Equal(t, prompt, monitor.TestPrompt())
	assert.Equal(t, uint(2048), monitor.TestMaxTokens())
	require.NoError(t, model.UpdateOption(operation_setting.ChannelTestPromptOptionKey, " \r\n "))
	assert.Equal(t, operation_setting.DefaultChannelTestPrompt, monitor.TestPrompt())
}

func TestChannelTestCapturesReadableOutput(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"chat", `{"choices":[{"message":{"content":"答案：391。"}}]}`, "答案：391。"},
		{"chat parts", `{"choices":[{"message":{"content":[{"type":"text","text":"first"},{"type":"image_url","image_url":{"url":"private"}},{"type":"text","text":" second"}]}}]}`, "first second"},
		{"responses", `{"output":[{"type":"reasoning","encrypted_content":"private"},{"type":"message","content":[{"type":"output_text","text":"391"}]}]}`, "391"},
		{"claude", `{"content":[{"type":"thinking","thinking":"private"},{"type":"text","text":"391"}]}`, "391"},
		{"gemini", `{"candidates":[{"content":{"parts":[{"thought":true,"text":"private"},{"text":"391"},{"inlineData":{"data":"private"}}]}}]}`, "391"},
		{"legacy completion", `{"choices":[{"text":"391"}]}`, "391"},
		{"refusal", `{"choices":[{"message":{"content":null,"refusal":"Cannot answer"}}]}`, "Cannot answer"},
		{"chat stream", "data: {\"choices\":[{\"delta\":{\"content\":\"39\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"1\"}}]}\n\ndata: [DONE]\n", "391"},
		{"responses stream once", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"391\"}\n\ndata: {\"type\":\"response.output_text.done\",\"text\":\"391\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"391\"}]}]}}\n", "391"},
		{"responses done without deltas", "data: {\"type\":\"response.output_text.done\",\"text\":\"391\"}\n", "391"},
		{"claude interrupted stream", "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"39\"}}\n\ndata: {\"type\":\"error\",\"error\":{\"message\":\"failed\"}}\n", "39"},
		{"gemini stream", "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"39\"}]}}]}\n\ndata: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"1\"}]}}]}\n", "391"},
		{"no textual answer", `{"data":[{"embedding":[1,2,3]}]}`, ""},
		{"malformed payload", "data: {invalid\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := captureChannelTestOutput("test prompt", []byte(tc.body))
			assert.Equal(t, "test prompt", got.Prompt)
			assert.Equal(t, tc.want, got.Output)
			assert.False(t, got.OutputTruncated)
		})
	}
	answer := strings.Repeat("答", 11000)
	body := `{"choices":[{"message":{"content":` + common.GetJsonString(answer) + `}}]}`
	got := captureChannelTestOutput("长回答", []byte(body))
	assert.Equal(t, strings.Repeat("答", 10922), got.Output)
	assert.True(t, got.OutputTruncated)
	assert.True(t, utf8.ValidString(got.Output))
	// The old 8 KiB diagnostic preview must not truncate a test answer.
	answer = strings.Repeat("答", 3000)
	body = "data: {\"choices\":[{\"delta\":{\"content\":" + common.GetJsonString(answer) + "}}]}\n\n"
	got = captureChannelTestOutput("long stream", []byte(body))
	assert.Equal(t, answer, got.Output)
	assert.False(t, got.OutputTruncated)
}

func TestChannelTestPromptAndResponseLog(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	db := setupChannelProbeTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	withTieredBillingConfig(t, map[string]string{"gpt-4o-mini": "tiered_expr"}, map[string]string{"gpt-4o-mini": "p + c"})
	monitor := operation_setting.GetMonitorSetting()
	previous := *monitor
	oldConsume, oldError, oldExport := common.LogConsumeEnabled, constant.ErrorLogEnabled, common.DataExportEnabled
	common.LogConsumeEnabled, constant.ErrorLogEnabled, common.DataExportEnabled = true, true, false
	t.Cleanup(func() {
		*monitor = previous
		common.LogConsumeEnabled, constant.ErrorLogEnabled, common.DataExportEnabled = oldConsume, oldError, oldExport
	})
	const prompt = "请计算 17×23，说明\"理由\"。\n保留换行 🧪"
	const answer = "391。\n17×20 + 17×3 = 340 + 51。"
	monitor.ChannelTestPrompt, monitor.ChannelTestMaxTokens = prompt, 2048
	root := model.User{Username: "test-output-owner", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, db.Create(&root).Error)
	answerJSON := common.GetJsonString(answer)
	chatBody := `{"id":"test","model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":` + answerJSON + `},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`
	responsesBody := `{"id":"test","object":"response","model":"gpt-4o-mini","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":` + answerJSON + `}]}],"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30}}`
	for _, tc := range []struct {
		name, endpoint, promptPath, limitPath, body string
		channelType, status                         int
		stream                                      bool
	}{
		{"chat", "openai", "messages.0.content", "max_tokens", chatBody, constant.ChannelTypeOpenAI, 200, false},
		{"chat stream", "openai", "messages.0.content", "max_tokens", "data: {\"id\":\"test\",\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{\"content\":" + answerJSON + "},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":20,\"total_tokens\":30}}\n\ndata: [DONE]\n\n", constant.ChannelTypeOpenAI, 200, true},
		{"responses", "openai-response", "input.0.content", "max_output_tokens", responsesBody, constant.ChannelTypeOpenAI, 200, false},
		{"responses stream", "openai-response", "input.0.content", "max_output_tokens", "data: {\"type\":\"response.output_text.delta\",\"delta\":" + answerJSON + "}\n\ndata: {\"type\":\"response.completed\",\"response\":" + responsesBody + "}\n\n", constant.ChannelTypeOpenAI, 200, true},
		{"codex stream", "openai-response", "input.0.content", "", "data: {\"type\":\"response.completed\",\"response\":" + responsesBody + "}\n\n", constant.ChannelTypeCodex, 200, true},
		{"claude", "anthropic", "messages.0.content", "max_tokens", `{"id":"test","type":"message","role":"assistant","model":"gpt-4o-mini","content":[{"type":"text","text":` + answerJSON + `}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":20}}`, constant.ChannelTypeAnthropic, 200, false},
		{"gemini", "gemini", "contents.0.parts.0.text", "generationConfig.maxOutputTokens", `{"candidates":[{"content":{"role":"model","parts":[{"text":` + answerJSON + `}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":20,"totalTokenCount":30}}`, constant.ChannelTypeGemini, 200, false},
		{"upstream failure", "openai", "messages.0.content", "max_tokens", `{"error":{"message":"temporary test failure","type":"server_error"}}`, constant.ChannelTypeOpenAI, 502, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				assert.Equal(t, prompt, gjson.GetBytes(body, tc.promptPath).String(), string(body))
				if tc.limitPath != "" {
					assert.EqualValues(t, 2048, gjson.GetBytes(body, tc.limitPath).Int(), string(body))
				} else {
					assert.False(t, gjson.GetBytes(body, "max_output_tokens").Exists(), "Codex does not accept an output limit")
				}
				contentType := "application/json"
				if tc.stream {
					contentType = "text/event-stream"
				}
				w.Header().Set("Content-Type", contentType)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer upstream.Close()
			channel := &model.Channel{Name: tc.name, Type: tc.channelType, Key: "test-key", BaseURL: common.GetPointer(upstream.URL), Models: "gpt-4o-mini", Group: "default", Status: common.ChannelStatusEnabled}
			if tc.channelType == constant.ChannelTypeCodex {
				channel.Key = `{"access_token":"test-key","account_id":"test-account"}`
			}
			require.NoError(t, db.Create(channel).Error)
			result := testChannel(context.Background(), channel, root.Id, "gpt-4o-mini", tc.endpoint, tc.stream, true)
			wantOutput, wantType := answer, model.LogTypeConsume
			if tc.status == 200 {
				require.NoError(t, result.localErr)
				require.Nil(t, result.newAPIError)
			} else {
				require.Error(t, result.localErr)
				wantOutput, wantType = "", model.LogTypeError
			}
			var logs []model.Log
			require.NoError(t, model.LOG_DB.Where("channel_id = ?", channel.Id).Find(&logs).Error)
			require.Len(t, logs, 1)
			assert.Equal(t, wantType, logs[0].Type)
			assert.Equal(t, prompt, gjson.Get(logs[0].Other, "admin_info.channel_test.prompt").String())
			assert.Equal(t, wantOutput, gjson.Get(logs[0].Other, "admin_info.channel_test.output").String())
			assert.False(t, gjson.Get(logs[0].Other, "channel_test").Exists())
		})
	}
	userLogs, _, err := model.GetUserLogs(root.Id, model.LogTypeUnknown, 0, 0, "", "", 0, 100, "", "", "")
	require.NoError(t, err)
	require.NotEmpty(t, userLogs)
	for _, entry := range userLogs {
		assert.NotContains(t, entry.Other, "channel_test")
	}
}

func TestNodeChannelExclusionsRouteAndSkipHealthChecks(t *testing.T) {
	db := setupChannelProbeTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	oldMemory := common.MemoryCacheEnabled
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemory
		require.NoError(t, common.ConfigureNodeExcludedChannels(""))
		model.InitChannelCache()
	})
	var channels []*model.Channel
	for i := range 3 {
		priority := int64(3 - i)
		channel := &model.Channel{Name: fmt.Sprintf("node-channel-%d", i), Type: constant.ChannelTypeOpenAI, Key: "key", Models: "gpt-4o-mini", Group: "default", Status: common.ChannelStatusEnabled, Priority: &priority}
		require.NoError(t, db.Create(channel).Error)
		require.NoError(t, db.Create(&model.Ability{ChannelId: channel.Id, Group: "default", Model: "gpt-4o-mini", Enabled: true, Priority: &priority}).Error)
		channels = append(channels, channel)
	}
	require.NoError(t, common.ConfigureNodeExcludedChannels(fmt.Sprintf(" %d, %d ", channels[0].Id, channels[0].Id)))
	for _, invalid := range []string{"0", "-1", "1,", "abc", "1;2", "999999999999999999999"} {
		require.Error(t, common.ConfigureNodeExcludedChannels(invalid))
		assert.True(t, common.IsChannelExcludedOnNode(channels[0].Id), "invalid config must not clear the previous exclusion")
	}
	common.MemoryCacheEnabled = true
	model.InitChannelCache()
	for _, memory := range []bool{false, true} {
		common.MemoryCacheEnabled = memory
		for retry := range 2 {
			selected, err := model.GetRandomSatisfiedChannel("default", "gpt-4o-mini", retry, nil)
			require.NoError(t, err)
			require.NotNil(t, selected)
			assert.Equal(t, channels[retry+1].Id, selected.Id, "memory=%t retry=%d", memory, retry)
		}
	}
	ok, kind := model.ChannelSatisfiesFilters(channels[0], "gpt-4o-mini", nil)
	assert.False(t, ok, "pinned and affinity channels must also be excluded")
	assert.Equal(t, "node_channel_excluded", string(kind))
	assert.Equal(t, channels[1:], selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeScheduledAll))
	assert.Equal(t, channelTestSummary{}, testChannelForHealthCheck(context.Background(), channels[0], 0, true, 1))
	result := testChannel(context.Background(), channels[0], 0, "", "", false, true)
	require.ErrorContains(t, result.localErr, "NODE_EXCLUDED_CHANNEL_IDS")
	var stored model.Channel
	require.NoError(t, db.First(&stored, channels[0].Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status, "local exclusion must not disable the shared channel")
	require.NoError(t, common.ConfigureNodeExcludedChannels(""))
	selected, err := model.GetRandomSatisfiedChannel("default", "gpt-4o-mini", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, channels[0].Id, selected.Id, "a node without exclusions can still select the highest-priority channel")
	require.NoError(t, common.ConfigureNodeExcludedChannels(fmt.Sprintf("%d,%d,%d", channels[0].Id, channels[1].Id, channels[2].Id)))
	for _, memory := range []bool{false, true} {
		common.MemoryCacheEnabled = memory
		selected, err := model.GetRandomSatisfiedChannel("default", "gpt-4o-mini", 0, nil)
		require.NoError(t, err)
		assert.Nil(t, selected, "no fallback to excluded channels when the local pool is empty")
	}
}

func TestChannelFailureKeywordMatching(t *testing.T) {
	oldEnabled, oldKeywords := common.AutomaticDisableChannelEnabled, operation_setting.AutomaticDisableKeywordsToString()
	common.AutomaticDisableChannelEnabled = true
	operation_setting.AutomaticDisableKeywordsFromString(" Insufficient CREDIT\r\n余额不足\n\n")
	t.Cleanup(func() {
		common.AutomaticDisableChannelEnabled = oldEnabled
		operation_setting.AutomaticDisableKeywordsFromString(oldKeywords)
	})
	for _, tc := range []struct {
		message         string
		skipRetry, want bool
	}{
		{"upstream: insufficient credit for this call", false, true},
		{"供应商账号余额不足，请充值", false, true},
		{"INSUFFICIENT CREDIT", true, true},
		{"invalid prompt", false, false},
	} {
		err := relaytypes.NewOpenAIError(errors.New(tc.message), relaytypes.ErrorCodeBadResponseStatusCode, http.StatusBadRequest)
		if tc.skipRetry {
			err = relaytypes.NewOpenAIError(errors.New(tc.message), relaytypes.ErrorCodeBadResponseStatusCode, http.StatusBadRequest, relaytypes.ErrOptionWithSkipRetry())
		}
		assert.Equal(t, tc.want, service.ShouldDisableChannel(err), tc.message)
	}
	common.AutomaticDisableChannelEnabled = false
	assert.False(t, service.ShouldDisableChannel(relaytypes.NewOpenAIError(errors.New("余额不足"), relaytypes.ErrorCodeBadResponseStatusCode, http.StatusBadRequest)))
}

func TestPassiveChannelRecoveryEnablesHealthyChannel(t *testing.T) {
	for _, multiKey := range []bool{false, true} {
		t.Run(fmt.Sprintf("multi_key_%t", multiKey), func(t *testing.T) {
			initModelListColumnNames(t)
			db := setupManageUserTestDB(t)
			require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
			withTieredBillingConfig(t, map[string]string{"gpt-4o-mini": "tiered_expr"}, map[string]string{"gpt-4o-mini": "p + c"})
			oldEnable, oldDisable, oldMemory := common.AutomaticEnableChannelEnabled, common.AutomaticDisableChannelEnabled, common.MemoryCacheEnabled
			common.AutomaticEnableChannelEnabled, common.AutomaticDisableChannelEnabled, common.MemoryCacheEnabled = true, false, false
			t.Cleanup(func() {
				common.AutomaticEnableChannelEnabled, common.AutomaticDisableChannelEnabled, common.MemoryCacheEnabled = oldEnable, oldDisable, oldMemory
			})
			root := model.User{Username: "recovery-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default"}
			require.NoError(t, db.Create(&root).Error)
			var requests atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				assert.Equal(t, "Bearer recovery-key", r.Header.Get("Authorization"))
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"health-check","object":"chat.completion","model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
			}))
			t.Cleanup(upstream.Close)
			channel := &model.Channel{Name: "recoverable", Type: constant.ChannelTypeOpenAI, Key: "recovery-key", BaseURL: common.GetPointer(upstream.URL), Models: "gpt-4o-mini", Group: "default", Status: common.ChannelStatusAutoDisabled}
			if multiKey {
				channel.Key += "\nmanual-key"
				channel.ChannelInfo = model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2, MultiKeyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled, 1: common.ChannelStatusManuallyDisabled}}
			}
			require.NoError(t, db.Create(channel).Error)
			require.NoError(t, db.Create(&model.Ability{ChannelId: channel.Id, Group: "default", Model: "gpt-4o-mini", Enabled: false}).Error)

			summary := testChannelForHealthCheck(context.Background(), channel, root.Id, false, 60000)

			assert.Equal(t, channelTestSummary{Tested: 1, Succeeded: 1, Enabled: 1}, summary)
			assert.EqualValues(t, 1, requests.Load())
			require.NoError(t, db.First(channel, channel.Id).Error)
			assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
			if multiKey {
				assert.NotContains(t, channel.ChannelInfo.MultiKeyStatusList, 0)
				assert.Equal(t, common.ChannelStatusManuallyDisabled, channel.ChannelInfo.MultiKeyStatusList[1])
			}
			var ability model.Ability
			require.NoError(t, db.First(&ability).Error)
			assert.True(t, ability.Enabled)
		})
	}
}

func TestValidateChannelProxy(t *testing.T) {
	tests := []struct {
		name    string
		proxy   string
		wantErr bool
	}{
		{name: "empty"},
		{name: "http", proxy: "http://proxy.example:8080"},
		{name: "https", proxy: "https://proxy.example:8443"},
		{name: "socks5", proxy: "socks5://proxy.example"},
		{name: "socks5h", proxy: "socks5h://proxy.example:1080/"},
		{name: "unsupported", proxy: "ftp://proxy.example", wantErr: true},
		{name: "path", proxy: "socks5://proxy.example:1080/path", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setting, err := common.Marshal(dto.ChannelSettings{Proxy: test.proxy})
			require.NoError(t, err)
			channel := &model.Channel{
				Type:    constant.ChannelTypeOpenAI,
				Setting: common.GetPointer(string(setting)),
			}

			err = validateChannel(channel, false)

			if test.wantErr {
				require.ErrorContains(t, err, "invalid channel proxy")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateChannelRequiresNewAPIBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL *string
		wantErr bool
	}{
		{name: "missing", wantErr: true},
		{name: "blank", baseURL: common.GetPointer("  "), wantErr: true},
		{name: "configured", baseURL: common.GetPointer("https://new-api.example")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			channel := &model.Channel{
				Type:    constant.ChannelTypeNewAPI,
				BaseURL: test.baseURL,
			}

			err := validateChannel(channel, false)

			if test.wantErr {
				require.ErrorContains(t, err, "New API channel base URL cannot be empty")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestNewAPIChannelRegistration(t *testing.T) {
	apiType, ok := common.ChannelType2APIType(constant.ChannelTypeNewAPI)

	require.True(t, ok)
	assert.Equal(t, constant.APITypeNewAPI, apiType)
	assert.Equal(t, "New API", constant.GetChannelTypeName(constant.ChannelTypeNewAPI))
	require.Greater(t, len(constant.ChannelBaseURLs), constant.ChannelTypeNewAPI)
	assert.Empty(t, constant.ChannelBaseURLs[constant.ChannelTypeNewAPI])
}

func TestResponsesCompactChannelSupport(t *testing.T) {
	tests := []struct {
		name        string
		channelType int
		apiType     int
		want        bool
	}{
		{name: "OpenAI", channelType: constant.ChannelTypeOpenAI, apiType: constant.APITypeOpenAI, want: true},
		{name: "Azure", channelType: constant.ChannelTypeAzure, apiType: constant.APITypeOpenAI, want: true},
		{name: "Codex", channelType: constant.ChannelTypeCodex, apiType: constant.APITypeCodex, want: true},
		{name: "Advanced Custom", channelType: constant.ChannelTypeAdvancedCustom, apiType: constant.APITypeAdvancedCustom, want: true},
		{name: "Sub2API", channelType: constant.ChannelTypeSub2API, apiType: constant.APITypeSub2API, want: true},
		{name: "New API", channelType: constant.ChannelTypeNewAPI, apiType: constant.APITypeNewAPI, want: true},
		{name: "Anthropic", channelType: constant.ChannelTypeAnthropic, apiType: constant.APITypeAnthropic, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, common.SupportsResponsesCompact(test.channelType, test.apiType))
		})
	}
}

func TestMultiprotocolGatewayEndpointTypes(t *testing.T) {
	want := []constant.EndpointType{
		constant.EndpointTypeOpenAI,
		constant.EndpointTypeOpenAIResponse,
		constant.EndpointTypeOpenAIResponseCompact,
		constant.EndpointTypeAnthropic,
		constant.EndpointTypeGemini,
		constant.EndpointTypeOpenAIAlphaSearch,
	}

	assert.Equal(t, want, common.GetEndpointTypesByChannelType(constant.ChannelTypeNewAPI, "gpt-5"))
	assert.Equal(t, want, common.GetEndpointTypesByChannelType(constant.ChannelTypeSub2API, "gpt-5"))
}

func TestCopyChannelRejectsInvalidLegacyProxySettings(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	settingBytes, err := common.Marshal(dto.ChannelSettings{
		Proxy: "socks5://proxy.example/legacy-path",
	})
	require.NoError(t, err)
	setting := string(settingBytes)
	origin := &model.Channel{
		Type:    constant.ChannelTypeOpenAI,
		Name:    "legacy proxy channel",
		Key:     "test-key",
		Models:  "gpt-test",
		Group:   "default",
		Setting: &setting,
	}
	require.NoError(t, db.Create(origin).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", origin.Id)}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/copy", nil)

	CopyChannel(ctx)

	assert.Contains(t, recorder.Body.String(), "invalid channel settings")
	var channelCount int64
	require.NoError(t, db.Model(&model.Channel{}).Count(&channelCount).Error)
	assert.Equal(t, int64(1), channelCount)
}

func TestDeleteChannelResetsProxyCacheWhenPreReadFails(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	service.ResetProxyClientCache()
	t.Cleanup(service.ResetProxyClientCache)

	proxyURL := "http://proxy.example:8080"
	beforeDelete, err := service.GetHttpClientWithProxy(proxyURL)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "999999"}}
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/999999", nil)

	DeleteChannel(ctx)

	assert.Contains(t, recorder.Body.String(), `"success":true`)
	afterDelete, err := service.GetHttpClientWithProxy(proxyURL)
	require.NoError(t, err)
	assert.NotSame(t, beforeDelete, afterDelete)
}

func TestDeleteChannelBatchReportsAndAuditsActualDeletedCount(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	channel := &model.Channel{Name: "existing", Key: "test-key"}
	require.NoError(t, db.Create(channel).Error)

	requestBody, err := common.Marshal(ChannelBatch{Ids: []int{channel.Id, 999999}})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/batch", bytes.NewReader(requestBody))
	ctx.Request.Header.Set("Content-Type", "application/json")

	DeleteChannelBatch(ctx)

	var response struct {
		Success bool  `json:"success"`
		Data    int64 `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, int64(1), response.Data)

	var auditLog model.AuditLog
	require.NoError(t, db.Order("id desc").First(&auditLog).Error)
	var auditData struct {
		Operation struct {
			Params map[string]any `json:"params"`
		} `json:"op"`
	}
	encodedAudit, err := common.Marshal(auditLog.Other)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(encodedAudit, &auditData))
	assert.Equal(t, float64(1), auditData.Operation.Params["count"])
}

func TestSettleTestQuotaUsesTieredBilling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:   "tiered_expr",
			ExprString:    `param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`,
			ExprHash:      billingexpr.ExprHashString(`param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`),
			GroupRatio:    1,
			EstimatedTier: "stream",
			QuotaPerUnit:  common.QuotaPerUnit,
			ExprVersion:   1,
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"stream":true}`),
		},
	}

	quota, result := settleTestQuota(info, types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 2,
	}, &dto.Usage{
		PromptTokens: 1000,
	})

	require.Equal(t, 1500, quota)
	require.NotNil(t, result)
	require.Equal(t, "stream", result.MatchedTier)
}

func TestBuildTestLogOtherInjectsTieredInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  `tier("base", p * 2)`,
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	priceData := types.PriceData{
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	usage := &dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 12,
		},
	}

	requestRules := []billingexpr.RequestRuleTrace{{
		Cond:       `param("service_tier") == "fast"`,
		Multiplier: 2,
		Matched:    true,
	}}
	other := buildTestLogOther(ctx, info, priceData, usage, &billingexpr.TieredResult{
		MatchedTier:  "base",
		RequestRules: requestRules,
	})

	fields := other.Snapshot()
	require.Equal(t, "tiered_expr", fields["billing_mode"])
	require.Equal(t, "base", fields["matched_tier"])
	require.Equal(t, requestRules, fields["request_rules"])
	require.NotEmpty(t, fields["expr_b64"])
}

func TestResolveChannelTestUserIDUsesRequestUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", 2)

	userID, err := resolveChannelTestUserID(ctx)

	require.NoError(t, err)
	require.Equal(t, 2, userID)
}

func TestSelectChannelsForAutomaticTestPassiveRecoveryOnlyUsesAutoDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModePassiveRecovery)

	require.Len(t, selected, 1)
	require.Equal(t, 2, selected[0].Id)
}

func TestSelectChannelsForAutomaticTestScheduledSkipsManualDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeScheduledAll)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Id)
	require.Equal(t, 2, selected[1].Id)
}

func TestSelectChannelsForAutomaticTestAutoBanOnlyUsesEligibleChannels(t *testing.T) {
	autoBanEnabled := 1
	autoBanDisabled := 0
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled, AutoBan: &autoBanEnabled},
		{Id: 2, Status: common.ChannelStatusEnabled, AutoBan: &autoBanDisabled},
		{Id: 3, Status: common.ChannelStatusAutoDisabled, AutoBan: &autoBanEnabled},
		{Id: 4, Status: common.ChannelStatusManuallyDisabled, AutoBan: &autoBanEnabled},
		{Id: 5, Status: common.ChannelStatusEnabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeAutoBanOnly)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Id)
	require.Equal(t, 3, selected[1].Id)
}

func TestRunChannelTestWorkersHonorsConfiguredConcurrency(t *testing.T) {
	originalInterval := common.RequestInterval
	common.RequestInterval = 0
	t.Cleanup(func() { common.RequestInterval = originalInterval })

	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusEnabled},
		{Id: 3, Status: common.ChannelStatusEnabled},
		{Id: 4, Status: common.ChannelStatusEnabled},
	}
	started := make(chan struct{}, len(channels))
	release := make(chan struct{})
	var active atomic.Int32
	var maxActive atomic.Int32
	progress := make([]int, 0, len(channels)+1)
	summaryResult := make(chan channelTestSummary, 1)

	go func() {
		summaryResult <- runChannelTestWorkers(
			context.Background(),
			channels,
			2,
			func(_ context.Context, _ *model.Channel) channelTestSummary {
				current := active.Add(1)
				defer active.Add(-1)
				for {
					observed := maxActive.Load()
					if current <= observed || maxActive.CompareAndSwap(observed, current) {
						break
					}
				}
				started <- struct{}{}
				<-release
				return channelTestSummary{Tested: 1, Succeeded: 1}
			},
			func(processed, _ int) {
				progress = append(progress, processed)
			},
		)
	}()

	<-started
	<-started
	select {
	case <-started:
		t.Fatal("started more channel tests than the configured concurrency")
	default:
	}
	close(release)

	summary := <-summaryResult

	assert.Equal(t, int32(2), maxActive.Load())
	assert.Equal(t, channelTestSummary{Tested: 4, Succeeded: 4}, summary)
	assert.Equal(t, []int{0, 1, 2, 3, 4}, progress)
}

func TestRunChannelTestWorkersStopsAfterCancellation(t *testing.T) {
	originalInterval := common.RequestInterval
	common.RequestInterval = 0
	t.Cleanup(func() { common.RequestInterval = originalInterval })

	ctx, cancel := context.WithCancel(context.Background())
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusEnabled},
		{Id: 3, Status: common.ChannelStatusEnabled},
		{Id: 4, Status: common.ChannelStatusEnabled},
	}
	started := make(chan struct{}, len(channels))
	progress := make([]int, 0, 1)
	summaryResult := make(chan channelTestSummary, 1)

	go func() {
		summaryResult <- runChannelTestWorkers(
			ctx,
			channels,
			2,
			func(ctx context.Context, _ *model.Channel) channelTestSummary {
				started <- struct{}{}
				<-ctx.Done()
				return channelTestSummary{Tested: 1, Succeeded: 1}
			},
			func(processed, _ int) {
				progress = append(progress, processed)
			},
		)
	}()

	<-started
	<-started
	cancel()

	summary := <-summaryResult

	select {
	case <-started:
		t.Fatal("started another channel test after cancellation")
	default:
	}
	assert.Equal(t, channelTestSummary{Tested: 2, Succeeded: 2}, summary)
	assert.Equal(t, []int{0}, progress)
}

func TestTestAllChannelsRejectsExistingActiveTask(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))

	existing, err := model.CreateSystemTask(model.SystemTaskTypeChannelTest, nil, nil)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/test", nil)

	TestAllChannels(ctx)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), existing.TaskID)
	require.Contains(t, recorder.Body.String(), "已有通道测试任务正在运行或等待中")
}

func TestAvailableModelTestsActuallyProbeAndFailOver(t *testing.T) {
	db := setupChannelProbeTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	withTieredBillingConfig(t, map[string]string{"gpt-4o-mini": "tiered_expr"}, map[string]string{"gpt-4o-mini": "p + c"})
	monitor := operation_setting.GetMonitorSetting()
	previous, oldTimeout := *monitor, constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { *monitor = previous; constant.StreamingTimeout = oldTimeout })
	monitor.ChannelTestModels = "gpt-4o-mini"
	root := model.User{Username: "available-model-test", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, db.Create(&root).Error)
	var hits [3]atomic.Int32
	var allFail atomic.Bool
	for i := range 3 {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits[i].Add(1)
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Errorf("availability test selected a disabled key")
			}
			if i == 0 || allFail.Load() {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadGateway)
				_, _ = io.WriteString(w, `{"error":{"message":"unavailable","type":"server_error"}}`)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"test","model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"391"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11}}`)
		}))
		t.Cleanup(upstream.Close)
		priority := int64(3 - i)
		channel := model.Channel{Name: fmt.Sprintf("available-%d", i), Type: constant.ChannelTypeOpenAI, Key: "test-key", BaseURL: &upstream.URL, Models: "gpt-4o-mini", Group: "default", Status: common.ChannelStatusEnabled, Priority: &priority}
		channel.Key = "disabled-key\ntest-key"
		channel.ChannelInfo.IsMultiKey = true
		channel.ChannelInfo.MultiKeySize = 2
		channel.ChannelInfo.MultiKeyStatusList = map[int]int{0: common.ChannelStatusAutoDisabled}
		require.NoError(t, db.Create(&channel).Error)
		require.NoError(t, channel.AddAbilities(nil))
	}
	summary, err := runAvailableModelTests(t.Context(), root.Id, false, nil)
	require.NoError(t, err)
	assert.Equal(t, channelTestSummary{Tested: 2, Succeeded: 1, Failed: 1}, summary)
	assert.Equal(t, int32(1), hits[0].Load())
	assert.Equal(t, int32(1), hits[1].Load())
	assert.Zero(t, hits[2].Load(), "stop after the successful upstream")
	allFail.Store(true)
	summary, err = runAvailableModelTests(t.Context(), root.Id, false, nil)
	require.NoError(t, err)
	assert.Equal(t, channelTestSummary{Tested: 3, Failed: 3}, summary)
}

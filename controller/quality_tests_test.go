package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func qualityFixture(t *testing.T, external bool) (*gorm.DB, model.QualityTarget) {
	t.Helper()
	oldDB, oldLog := model.DB, model.LOG_DB
	oldMain, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	oldRedis, oldMemory := common.RedisEnabled, common.MemoryCacheEnabled
	common.RedisEnabled, common.MemoryCacheEnabled = false, false
	dialect := common.DatabaseTypeSQLite
	var driver gorm.Dialector = sqlite.Open(filepath.Join(t.TempDir(), "quality.db") + "?_pragma=busy_timeout(30000)&_txlock=immediate")
	if external {
		switch os.Getenv("QUALITY_DB_DIALECT") {
		case "mysql":
			driver = mysql.Open(os.Getenv("QUALITY_DB_DSN"))
			dialect = common.DatabaseTypeMySQL
		case "postgres":
			driver = postgres.Open(os.Getenv("QUALITY_DB_DSN"))
			dialect = common.DatabaseTypePostgreSQL
		}
	}
	db, err := gorm.Open(driver, &gorm.Config{})
	require.NoError(t, err)
	common.SetDatabaseTypes(dialect, dialect)
	model.DB, model.LOG_DB = db, db
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	schema := []any{&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.Option{}, &model.SystemTask{}, &model.SystemTaskLock{}, &model.QualityTest{}, &model.QualityResult{}, &model.Log{}, &model.UserSubscription{}}
	require.NoError(t, db.AutoMigrate(schema...))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	if dialect == common.DatabaseTypeSQLite {
		sqlDB.SetMaxOpenConns(1)
	}
	oldUsable, oldRatios, oldAuto := setting.UserUsableGroups2JSONString(), ratio_setting.GroupRatio2JSONString(), setting.AutoGroups2JsonString()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP","auto":"Auto"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1}`))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`[]`))
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLog
		common.SetDatabaseTypes(oldMain, oldLogType)
		common.RedisEnabled, common.MemoryCacheEnabled = oldRedis, oldMemory
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(oldUsable))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldRatios))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(oldAuto))
		require.NoError(t, sqlDB.Close())
	})
	user := model.User{Username: "quality-admin-" + common.GetRandomString(8), Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default", Quota: 10000000, AffCode: common.GetRandomString(8)}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: common.GetRandomString(48), Name: "quality-key", Status: common.TokenStatusEnabled, ExpiredTime: -1, Group: "auto", UnlimitedQuota: true}
	require.NoError(t, token.SetAutoGroups([]string{"default", "vip"}))
	require.NoError(t, db.Create(&token).Error)
	priority := int64(0)
	channel := model.Channel{Name: "quality-model", Type: constant.ChannelTypeOpenAI, Key: "upstream-secret", Models: "gpt-4o-mini", Group: "default,vip", Status: common.ChannelStatusEnabled, Priority: &priority}
	require.NoError(t, db.Create(&channel).Error)
	for _, group := range []string{"default", "vip"} {
		require.NoError(t, db.Create(&model.Ability{Group: group, Model: "gpt-4o-mini", ChannelId: channel.Id, Enabled: true, Priority: &priority}).Error)
	}
	return db, model.QualityTarget{OwnerID: user.Id, TokenID: token.Id, Model: "gpt-4o-mini", Group: "vip", Endpoint: "chat"}
}

func qualityLease(t *testing.T) (*model.SystemTask, string) {
	t.Helper()
	task, err := model.CreateSystemTask(model.QualityTaskType, nil, nil)
	require.NoError(t, err)
	runner := "quality-test-runner"
	claimed, ok, err := model.ClaimSystemTask(task.ID, model.QualityTaskType, runner, time.Now().Unix()+600)
	require.NoError(t, err)
	require.True(t, ok)
	return claimed, runner
}

func qualityAPI(t *testing.T, handler gin.HandlerFunc, path string, admin bool, body any, id string) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := common.Marshal(body)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("quality_admin", admin)
	if id != "" {
		c.Params = gin.Params{{Key: "id", Value: id}}
	}
	handler(c)
	return recorder
}

func TestQualityExecutionAndJudgement(t *testing.T) {
	for _, tc := range []struct {
		name, answer, judge, status, stage, endpoint string
		code, calls                                  int
	}{
		{"exact normalized", "  42\r\n", "", "passed", "", "chat", 200, 1},
		{"semantic", "The answer is 42", `{"verdict":"correct","reason":"Equivalent answer"}`, "passed", "", "chat", 200, 2},
		{"incorrect", "41", `{"verdict":"incorrect","reason":"Incorrect calculation"}`, "pending", "", "chat", 200, 2},
		{"uncertain", "Maybe", `{"verdict":"uncertain","reason":"Insufficient evidence"}`, "pending", "", "chat", 200, 2},
		{"invalid judge", "41", "correct", "pending", "judge", "chat", 200, 2},
		{"empty answer", "", "", "failed", "test", "chat", 200, 1},
		{"upstream failure", "", "", "failed", "test", "chat", 503, 1},
		{"responses", "42", "", "passed", "", "responses", 200, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, target := qualityFixture(t, false)
			target.Endpoint = tc.endpoint
			task, runner := qualityLease(t)
			test := model.QualityTest{Name: "Arithmetic", QualityTarget: target, Prompt: "6*7?", ExpectedAnswer: "42", IntervalMinutes: 30, RequestedAt: time.Now().Unix()}
			require.NoError(t, db.Create(&test).Error)
			result, err := model.ClaimQualityTest(test.ID, model.QualityJudge{QualityTarget: target, Instructions: "Evaluate arithmetic"}, time.Now().Unix(), task.TaskID, runner)
			require.NoError(t, err)
			require.NotNil(t, result)
			calls := 0
			h := qualityTestHandler{relay: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				metadata, ok := common.GetQualityRequest(r.Context())
				assert.True(t, ok)
				assert.Equal(t, "vip", metadata.Group)
				assert.NotEmpty(t, r.Header.Get("Authorization"))
				answer := tc.answer
				if calls == 2 {
					answer = tc.judge
					assert.Equal(t, "judge", metadata.Stage)
				}
				w.Header().Set(common.RequestIdKey, fmt.Sprintf("request-%d", calls))
				w.WriteHeader(tc.code)
				response := map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": answer}}}}
				if tc.endpoint == "responses" {
					assert.Equal(t, "/v1/responses", r.URL.Path)
					response = map[string]any{"output": []any{map[string]any{"type": "message", "content": []any{map[string]string{"type": "output_text", "text": answer}}}}}
				}
				data, err := common.Marshal(response)
				require.NoError(t, err)
				_, _ = w.Write(data)
			})}
			h.execute(context.Background(), result)
			var stored model.QualityResult
			require.NoError(t, db.First(&stored, result.ID).Error)
			assert.Equal(t, tc.status, stored.Status)
			assert.Equal(t, tc.stage, stored.FailureStage)
			assert.Equal(t, tc.calls, calls)
			assert.NotZero(t, stored.FinishedAt)
		})
	}
}

func TestQualityPermissionsAndVisibility(t *testing.T) {
	db, target := qualityFixture(t, false)
	valid, err := service.ValidateQualityTarget(target)
	require.NoError(t, err)
	bad := target
	bad.Group = "forbidden"
	_, err = service.ValidateQualityTarget(bad)
	require.Error(t, err)
	engine := gin.New()
	engine.POST("/v1/chat/completions", middleware.TokenAuth(), func(c *gin.Context) {
		assert.Equal(t, "vip", common.GetContextKeyString(c, constant.ContextKeyUsingGroup))
		c.JSON(200, gin.H{"choices": []any{gin.H{"message": gin.H{"content": "42"}}}})
	})
	h := qualityTestHandler{relay: engine}
	answer, _, err := h.call(context.Background(), target, 1, "test", "", "question")
	require.NoError(t, err)
	assert.Equal(t, "42", answer)
	for _, tc := range []struct {
		name    string
		updates map[string]any
	}{
		{"expired", map[string]any{"expired_time": time.Now().Unix() - 1}},
		{"revoked", map[string]any{"status": common.TokenStatusDisabled}},
		{"empty quota", map[string]any{"unlimited_quota": false, "remain_quota": 0}},
		{"IP restriction", map[string]any{"allow_ips": "192.0.2.1"}},
		{"model restriction", map[string]any{"model_limits_enabled": true, "model_limits": "other-model"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, db.Model(&model.Token{}).Where("id = ?", valid.Id).Updates(tc.updates).Error)
			_, _, err := h.call(context.Background(), target, 1, "test", "", "question")
			assert.Error(t, err)
			require.NoError(t, db.Model(&model.Token{}).Where("id = ?", valid.Id).Updates(map[string]any{"expired_time": -1, "status": common.TokenStatusEnabled, "unlimited_quota": true, "allow_ips": "", "model_limits_enabled": false}).Error)
		})
	}
	tests := []model.QualityTest{{Name: "public", Public: true, QualityTarget: target}, {Name: "private", QualityTarget: target}}
	require.NoError(t, db.Create(&tests).Error)
	for _, test := range tests {
		require.NoError(t, db.Create(&model.QualityResult{TestID: test.ID, Status: "passed", Answer: "42", Snapshot: "private config", Error: "internal", RequestID: "private-request", StartedAt: time.Now().Unix()}).Error)
	}
	rec := qualityAPI(t, ListQualityResults, "/results", false, nil, "")
	assert.Equal(t, int64(1), gjson.GetBytes(rec.Body.Bytes(), "data.total").Int())
	assert.NotContains(t, rec.Body.String(), "private config")
	assert.NotContains(t, rec.Body.String(), "private-request")
	var hidden model.QualityResult
	require.NoError(t, db.Where("test_id = ?", tests[1].ID).First(&hidden).Error)
	rec = qualityAPI(t, GetQualityResult, "/results", false, nil, fmt.Sprint(hidden.ID))
	assert.Equal(t, 404, rec.Code)
	// Hiding/stopping remains possible after the referenced key disappears.
	require.NoError(t, db.Delete(&model.Token{}, valid.Id).Error)
	rec = qualityAPI(t, UpdateQualityTestFlags, "/test", true, gin.H{"public": false, "enabled": false}, fmt.Sprint(tests[0].ID))
	assert.Equal(t, 200, rec.Code)
	rec = qualityAPI(t, ListQualityResults, "/results", false, nil, "")
	assert.Zero(t, gjson.GetBytes(rec.Body.Bytes(), "data.total").Int())
}

func TestQualitySchedulingSnapshotsAndLeases(t *testing.T) {
	db, target := qualityFixture(t, false)
	task, runner := qualityLease(t)
	now := time.Now().Unix()
	test := model.QualityTest{Name: "schedule", QualityTarget: target, Prompt: "Q", ExpectedAnswer: "A", IntervalMinutes: 30, Enabled: true}
	require.NoError(t, model.SaveQualityTest(&test))
	var persisted model.QualityTest
	require.NoError(t, db.First(&persisted, test.ID).Error)
	assert.InDelta(t, now+1800, persisted.NextRunAt, 2)
	result, err := model.ClaimQualityTest(test.ID, model.QualityJudge{}, now, task.TaskID, runner)
	require.NoError(t, err)
	assert.Nil(t, result)
	require.NoError(t, model.RequestQualityTest(test.ID))
	result, err = model.ClaimQualityTest(test.ID, model.QualityJudge{Instructions: "old judge"}, now, task.TaskID, runner)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Error(t, model.RequestQualityTest(test.ID))
	var after model.QualityTest
	require.NoError(t, db.First(&after, test.ID).Error)
	assert.Equal(t, persisted.NextRunAt, after.NextRunAt)
	test.Prompt = "new Q"
	require.NoError(t, model.SaveQualityTest(&test))
	assert.Contains(t, result.Snapshot, "old judge")
	assert.Equal(t, "Q", result.Prompt)
	// Expired owners cannot claim another call or overwrite its result.
	require.NoError(t, db.Model(&model.SystemTaskLock{}).Where("task_id = ?", task.TaskID).Update("locked_until", now-1).Error)
	result.Status = "passed"
	require.NoError(t, model.FinishQualityResult(result))
	var stored model.QualityResult
	require.NoError(t, db.First(&stored, result.ID).Error)
	assert.Equal(t, "running", stored.Status)
	_, err = model.ClaimQualityTest(test.ID, model.QualityJudge{}, now, task.TaskID, runner)
	require.ErrorIs(t, err, model.ErrSystemTaskLockLost)
	require.NoError(t, model.ExpireStaleSystemTaskLocks(now))
	disabled := false
	require.NoError(t, model.UpdateQualityTestFlags(test.ID, &disabled, nil))
	h := qualityTestHandler{}
	assert.True(t, h.Enabled(), "interrupted manual runs must trigger recovery even with all schedules disabled")
	next, newRunner := qualityLease(t)
	h.Run(context.Background(), next, newRunner)
	require.NoError(t, db.First(&stored, result.ID).Error)
	assert.Equal(t, "failed", stored.Status)
	assert.Equal(t, "interrupted", stored.FailureStage)
	assert.NoError(t, model.RequestQualityTest(test.ID), "recovery permits a new manual run")
	// A resumed stale worker must not fail this new batch.
	newTask, newestRunner := qualityLease(t)
	newestResult, err := model.ClaimQualityTest(test.ID, model.QualityJudge{}, now, newTask.TaskID, newestRunner)
	require.NoError(t, err)
	require.NotNil(t, newestResult)
	h.Run(context.Background(), task, runner)
	var newest model.QualityResult
	require.NoError(t, db.First(&newest, newestResult.ID).Error)
	assert.Equal(t, "running", newest.Status)
}

func TestQualityDatabase(t *testing.T) {
	db, target := qualityFixture(t, true)
	// These tables are new in this change; rerunning migrations preserves rows,
	// indexes, false flags and large escaped execution snapshots on each dialect.
	test := model.QualityTest{Name: "migration", QualityTarget: target, Prompt: strings.Repeat("\n", 20000), ExpectedAnswer: strings.Repeat("\t", 20000), IntervalMinutes: 30, Public: true}
	require.NoError(t, model.SaveQualityTest(&test))
	task, runner := qualityLease(t)
	require.NoError(t, model.RequestQualityTest(test.ID))
	result, err := model.ClaimQualityTest(test.ID, model.QualityJudge{Instructions: strings.Repeat("\r", 20000)}, time.Now().Unix(), task.TaskID, runner)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Greater(t, len(result.Snapshot), 65535)
	for range 2 {
		require.NoError(t, db.AutoMigrate(&model.QualityTest{}, &model.QualityResult{}))
	}
	var stored model.QualityResult
	require.NoError(t, db.First(&stored, result.ID).Error)
	assert.Equal(t, result.Snapshot, stored.Snapshot)
	require.True(t, db.Migrator().HasIndex(&model.QualityResult{}, "idx_quality_history"))
	assert.False(t, test.Enabled)
	result.Status = "passed"
	require.NoError(t, model.FinishQualityResult(result))
	rec := qualityAPI(t, QualitySummary, "/summary", false, nil, "")
	require.Equal(t, 200, rec.Code)
	assert.Equal(t, int64(1), gjson.GetBytes(rec.Body.Bytes(), "data.counts.passed").Int())
	rec = qualityAPI(t, ListQualityTests, "/tests", false, nil, "")
	require.Equal(t, "passed", gjson.GetBytes(rec.Body.Bytes(), "data.0.latest_status").String())
	require.NoError(t, db.Delete(&test).Error)
	rec = qualityAPI(t, ListQualityResults, "/results", false, nil, "")
	assert.Zero(t, gjson.GetBytes(rec.Body.Bytes(), "data.total").Int())
	require.NoError(t, db.First(&stored, result.ID).Error)
	require.NoError(t, model.FinishSystemTask(task.TaskID, runner, model.SystemTaskStatusSucceeded, nil, ""))
}

func TestQualityNormalRelayBilling(t *testing.T) {
	db, target := qualityFixture(t, false)
	withTieredBillingConfig(t, map[string]string{"gpt-4o-mini": "tiered_expr"}, map[string]string{"gpt-4o-mini": "p + c"})
	oldLog, oldExport, oldRetry := common.LogConsumeEnabled, common.DataExportEnabled, common.RetryTimes
	common.LogConsumeEnabled, common.DataExportEnabled, common.RetryTimes = true, false, 0
	t.Cleanup(func() {
		common.LogConsumeEnabled, common.DataExportEnabled, common.RetryTimes = oldLog, oldExport, oldRetry
	})
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		call := calls.Add(1)
		assert.NotEmpty(t, gjson.GetBytes(data, "model").String())
		answer := "The answer is 42"
		if call == 2 {
			answer = `{"verdict":"correct","reason":"Equivalent"}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":"test","model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`, common.GetJsonString(answer))
	}))
	defer upstream.Close()
	require.NoError(t, db.Model(&model.Channel{}).Where("name = ?", "quality-model").Update("base_url", upstream.URL).Error)
	engine := gin.New()
	engine.Use(middleware.RequestId(), middleware.BodyStorageCleanup())
	engine.POST("/v1/chat/completions", middleware.TokenAuth(), middleware.Distribute(), func(c *gin.Context) { Relay(c, types.RelayFormatOpenAI) })
	task, runner := qualityLease(t)
	test := model.QualityTest{Name: "billing", QualityTarget: target, Prompt: "6*7?", ExpectedAnswer: "42", IntervalMinutes: 30, RequestedAt: time.Now().Unix()}
	require.NoError(t, db.Create(&test).Error)
	result, err := model.ClaimQualityTest(test.ID, model.QualityJudge{QualityTarget: target, Instructions: "Judge arithmetic"}, time.Now().Unix(), task.TaskID, runner)
	require.NoError(t, err)
	h := qualityTestHandler{relay: engine}
	h.execute(context.Background(), result)
	require.Equal(t, "passed", result.Status, result.Error)
	assert.EqualValues(t, 2, calls.Load())
	var user model.User
	require.NoError(t, db.First(&user, target.OwnerID).Error)
	assert.Less(t, user.Quota, 10000000)
	var token model.Token
	require.NoError(t, db.First(&token, target.TokenID).Error)
	assert.Positive(t, token.UsedQuota)
	var logs []model.Log
	require.NoError(t, db.Where("user_id = ? AND type = ?", target.OwnerID, model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, 2)
	assert.Equal(t, int64(result.ID), gjson.Get(logs[0].Other, "admin_info.quality_test.result_id").Int())
	assert.NotEqual(t, result.RequestID, result.JudgeRequestID)
}

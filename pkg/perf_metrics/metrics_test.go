package perfmetrics

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
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

func setupPerformanceTestDatabase(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	var driver gorm.Dialector
	if dialect == "sqlite" {
		driver = sqlite.Open(":memory:")
	} else {
		dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
		if dsn == "" {
			t.Skip("set TEST_" + strings.ToUpper(dialect) + "_DSN to run this database case")
		}
		if dialect == "mysql" {
			driver = mysql.Open(dsn)
		} else {
			driver = postgres.Open(dsn)
		}
	}
	db, err := gorm.Open(driver, &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	tables, err := db.Migrator().GetTables()
	require.NoError(t, err)
	require.Empty(t, tables, "performance tests require an empty disposable database")
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}))
	t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(&model.PerfMetric{})) })

	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousMaster, previousRedis := common.IsMasterNode, common.RedisEnabled
	t.Setenv("LOG_SQL_DSN", "")
	common.IsMasterNode, common.RedisEnabled = false, false
	model.DB = db
	databaseType := common.DatabaseType(dialect)
	common.SetDatabaseTypes(databaseType, databaseType)
	// Initialize the same dialect-specific columns used at application startup.
	require.NoError(t, model.InitLogDB())
	t.Cleanup(func() {
		hotBuckets.Clear()
		model.DB = previousDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		assert.NoError(t, model.InitLogDB())
		model.LOG_DB = previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		common.IsMasterNode, common.RedisEnabled = previousMaster, previousRedis
	})
	versionQuery := "SELECT version()"
	if dialect == "sqlite" {
		versionQuery = "SELECT sqlite_version()"
	}
	var version string
	require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
	t.Logf("database: %s %s", dialect, version)
	return db
}

func TestSummaryBestGroupDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := setupPerformanceTestDatabase(t, dialect)
			hour := time.Now().Unix()/3600*3600 - 3600
			for _, source := range []string{"database", "memory", "mixed"} {
				t.Run(source, func(t *testing.T) {
					require.NoError(t, db.Where("1 = 1").Delete(&model.PerfMetric{}).Error)
					hotBuckets.Clear()
					rows := []model.PerfMetric{
						{ModelName: "example-model", Group: "codex", BucketTs: hour, RequestCount: 1000, SuccessCount: 978, TotalLatencyMs: 60100000, TtftSumMs: 24310000, TtftCount: 1000, OutputTokens: 32500, GenerationMs: 1000000},
						{ModelName: "example-model", Group: "codex-hb", BucketTs: hour, RequestCount: 1000, SuccessCount: 617, TotalLatencyMs: 62980000, TtftSumMs: 35770000, TtftCount: 1000, OutputTokens: 70400, GenerationMs: 1000000},
						{ModelName: "example-model", Group: "default", BucketTs: hour, RequestCount: 250, SuccessCount: 250, TotalLatencyMs: 5820000, TtftSumMs: 3750000, TtftCount: 250, OutputTokens: 5000, GenerationMs: 250000},
						{ModelName: "example-model", Group: "default", BucketTs: hour + 300, RequestCount: 750, SuccessCount: 750, TotalLatencyMs: 17460000, TtftSumMs: 6750000, TtftCount: 750, OutputTokens: 39500, GenerationMs: 750000},
						{ModelName: "example-model", Group: "retired", BucketTs: hour, RequestCount: 1, TotalLatencyMs: 1, TtftSumMs: 1, TtftCount: 1, OutputTokens: 1000, GenerationMs: 1000},
						{ModelName: "example-model", Group: "default", BucketTs: hour - 48*3600, RequestCount: 1, TotalLatencyMs: 1, TtftSumMs: 1, TtftCount: 1, OutputTokens: 1000, GenerationMs: 1000},
					}
					for _, row := range rows {
						if source == "memory" || (source == "mixed" && row.BucketTs == hour+300) {
							bucket := &atomicBucket{}
							bucket.addCounters(counters{
								requestCount: row.RequestCount, successCount: row.SuccessCount,
								totalLatencyMs: row.TotalLatencyMs, ttftSumMs: row.TtftSumMs, ttftCount: row.TtftCount,
								outputTokens: row.OutputTokens, generationMs: row.GenerationMs,
							})
							hotBuckets.Store(bucketKey{model: row.ModelName, group: row.Group, bucketTs: row.BucketTs}, bucket)
							continue
						}
						require.NoError(t, db.Create(&row).Error)
					}
					result, err := QuerySummaryAll(24, []string{"codex", "codex-hb", "default"})
					require.NoError(t, err)
					require.Len(t, result.Models, 1)
					encoded, err := common.Marshal(result.Models[0])
					require.NoError(t, err)
					expected, err := common.Marshal(map[string]any{
						"model_name": "example-model", "avg_latency_ms": 48786, "avg_tps": 49.13, "success_rate": 86.5,
						"best_group":            map[string]any{"group": "default", "avg_ttft_ms": 10500, "avg_tps": 44.5},
						"recent_success_series": []map[string]any{{"ts": hour, "success_rate": 86.5}},
					})
					require.NoError(t, err)
					assert.JSONEq(t, string(expected), string(encoded))
					assert.EqualValues(t, 3000, result.Models[0].RequestCount)
					empty, err := QuerySummaryAll(24, []string{})
					require.NoError(t, err)
					assert.Empty(t, empty.Models)
				})
			}
		})
	}
}

func TestBestGroupPerformanceSelection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		totals map[bucketKey]counters
		want   map[string]*BestGroupPerformance
	}{
		{
			name: "no_stream_samples",
			totals: map[bucketKey]counters{
				{model: "model", group: "nonstream"}: {requestCount: 1, totalLatencyMs: 10, outputTokens: 100, generationMs: 10},
			},
			want: map[string]*BestGroupPerformance{},
		},
		{
			name: "selects_by_ttft_and_keeps_same_group_throughput",
			totals: map[bucketKey]counters{
				{model: "model", group: "nonstream"}:  {requestCount: 1, totalLatencyMs: 1, outputTokens: 1000, generationMs: 10},
				{model: "model", group: "fast-first"}: {requestCount: 1, totalLatencyMs: 70000, ttftSumMs: 10000, ttftCount: 1, outputTokens: 20, generationMs: 1000},
				{model: "model", group: "fast-total"}: {requestCount: 1, totalLatencyMs: 12000, ttftSumMs: 11000, ttftCount: 1, outputTokens: 100, generationMs: 1000},
			},
			want: map[string]*BestGroupPerformance{"model": {Group: "fast-first", AvgTtftMs: 10000, AvgTps: 20}},
		},
		{
			name: "weights_by_ttft_samples_instead_of_all_requests",
			totals: map[bucketKey]counters{
				{model: "model", group: "a"}: {requestCount: 100, ttftSumMs: 12000, ttftCount: 2, outputTokens: 20, generationMs: 1000},
				{model: "model", group: "b"}: {requestCount: 10, ttftSumMs: 15000, ttftCount: 5, outputTokens: 30, generationMs: 1000},
			},
			want: map[string]*BestGroupPerformance{"model": {Group: "b", AvgTtftMs: 3000, AvgTps: 30}},
		},
		{
			name: "stable_ties_and_independent_models",
			totals: map[bucketKey]counters{
				{model: "model", group: "z"}: {requestCount: 1, ttftSumMs: 1000, ttftCount: 1, outputTokens: 100, generationMs: 1000},
				{model: "model", group: "a"}: {requestCount: 1, ttftSumMs: 1000, ttftCount: 1, outputTokens: 20, generationMs: 1000},
				{model: "other", group: "z"}: {requestCount: 1, ttftSumMs: 500, ttftCount: 1, outputTokens: 50, generationMs: 1000},
			},
			want: map[string]*BestGroupPerformance{
				"model": {Group: "a", AvgTtftMs: 1000, AvgTps: 20},
				"other": {Group: "z", AvgTtftMs: 500, AvgTps: 50},
			},
		},
		{
			name: "preserves_throughput_precision_used_by_group_details",
			totals: map[bucketKey]counters{
				{model: "model", group: "default"}: {requestCount: 1, ttftSumMs: 1000, ttftCount: 1, outputTokens: 44449, generationMs: 1000000},
			},
			want: map[string]*BestGroupPerformance{"model": {Group: "default", AvgTtftMs: 1000, AvgTps: 44.449}},
		},
		{
			name: "does_not_borrow_throughput_when_fastest_group_has_no_usage",
			totals: map[bucketKey]counters{
				{model: "model", group: "fast"}: {requestCount: 1, ttftSumMs: 1000, ttftCount: 1},
				{model: "model", group: "slow"}: {requestCount: 1, ttftSumMs: 2000, ttftCount: 1, outputTokens: 100, generationMs: 1000},
			},
			want: map[string]*BestGroupPerformance{"model": {Group: "fast", AvgTtftMs: 1000}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, selectBestGroupPerformances(tc.totals))
		})
	}
}

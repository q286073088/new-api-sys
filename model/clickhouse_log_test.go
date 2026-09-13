package model

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClickHouseRetryLogVisibilityMigration(t *testing.T) {
	if os.Getenv("TEST_CLICKHOUSE_DSN") == "" {
		t.Skip("set TEST_CLICKHOUSE_DSN to an empty disposable ClickHouse database")
	}
	for _, upgrade := range []bool{false, true} {
		t.Run(fmt.Sprintf("upgrade_%t", upgrade), func(t *testing.T) {
			setupReferralDatabase(t, "sqlite", false)
			db, kind, err := chooseDB("TEST_CLICKHOUSE_DSN", true)
			require.NoError(t, err)
			require.Equal(t, common.DatabaseTypeClickHouse, kind)
			var tables []string
			require.NoError(t, db.Raw("SHOW TABLES").Scan(&tables).Error)
			require.Empty(t, tables, "ClickHouse tests require an empty disposable database")
			previousLogDB := LOG_DB
			LOG_DB = db
			common.SetLogDatabaseType(kind)
			initCol()
			t.Setenv("LOG_SQL_CLICKHOUSE_TTL_DAYS", "0")
			t.Cleanup(func() {
				assert.NoError(t, db.Exec("DROP TABLE IF EXISTS logs").Error)
				assert.NoError(t, db.Exec("DROP TABLE IF EXISTS audit_logs").Error)
				sqlDB, err := db.DB()
				if assert.NoError(t, err) {
					assert.NoError(t, sqlDB.Close())
				}
				LOG_DB = previousLogDB
				common.SetLogDatabaseType(common.DatabaseTypeSQLite)
				initCol()
			})
			var version string
			require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
			t.Logf("ClickHouse %s", version)
			legacy := Log{UserId: 3, TokenId: 9, Type: LogTypeError, Content: "historical failure", CreatedAt: common.GetTimestamp(), RequestId: "historical-request"}
			if upgrade {
				// The released MergeTree schema has no user-visibility marker.
				legacySchema := strings.Replace(clickHouseLogCreateTableSQL(0), "\thidden_for_user UInt8 DEFAULT 0,\n", "", 1)
				require.NoError(t, db.Exec(legacySchema).Error)
				require.NoError(t, db.Omit("HiddenForUser").Create(&legacy).Error)
			}
			for range 2 {
				require.NoError(t, migrateLOGDB())
			}
			if !upgrade {
				require.NoError(t, createLog(&legacy))
			}
			for _, entry := range []Log{
				{UserId: 3, TokenId: 9, Type: LogTypeError, HiddenForUser: true, Content: "intermediate failure", RequestId: "retry-request"},
				{UserId: 3, TokenId: 9, Type: LogTypeConsume, Quota: 12, Content: "final success", RequestId: "retry-request"},
				{UserId: 4, TokenId: 10, Type: LogTypeError, Content: "another account", RequestId: "other-request"},
			} {
				entry.CreatedAt = common.GetTimestamp()
				require.NoError(t, createLog(&entry))
			}
			for range 2 {
				require.NoError(t, migrateLOGDB())
			}
			userLogs, total, err := GetUserLogs(3, LogTypeUnknown, 0, 0, "", "", 0, 1, "", "retry-request", "")
			require.NoError(t, err)
			assert.EqualValues(t, 1, total)
			require.Len(t, userLogs, 1)
			assert.Equal(t, "final success", userLogs[0].Content)
			assert.Equal(t, 12, userLogs[0].Quota)
			tokenLogs, err := GetLogByTokenId(9)
			require.NoError(t, err)
			require.Len(t, tokenLogs, 2)
			assert.ElementsMatch(t, []string{"historical failure", "final success"}, []string{tokenLogs[0].Content, tokenLogs[1].Content})
			adminLogs, total, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 10, 0, "", "retry-request", "")
			require.NoError(t, err)
			assert.EqualValues(t, 2, total)
			assert.Len(t, adminLogs, 2)
			var schema string
			require.NoError(t, db.Raw("SHOW CREATE TABLE logs").Scan(&schema).Error)
			assert.Contains(t, schema, "PARTITION BY toYYYYMM(toDateTime(created_at))")
			assert.Contains(t, schema, "ORDER BY (created_at, request_id)")
		})
	}
}

func TestIsClickHouseDSN(t *testing.T) {
	cases := []struct {
		dsn  string
		want bool
	}{
		{"clickhouse://default:pass@localhost:9000/logs", true},
		{"tcp://localhost:9000/logs", true},
		{"http://localhost:8123/logs", true},
		{"https://localhost:8443/logs", true},
		{"postgres://root:pass@localhost:5432/db", false},
		{"postgresql://root:pass@localhost:5432/db", false},
		{"root:pass@tcp(localhost:3306)/db", false},
		{"local", false},
		{"", false},
	}
	for _, c := range cases {
		assert.Equalf(t, c.want, isClickHouseDSN(c.dsn), "dsn=%q", c.dsn)
	}
}

func TestNormalizeClickHouseDSN(t *testing.T) {
	// https without secure gets secure=true appended
	normalized := normalizeClickHouseDSN("https://default:pass@localhost:8443/logs")
	assert.Contains(t, normalized, "secure=true")
	assert.True(t, strings.HasPrefix(normalized, "https://"))

	// https that already specifies secure is left untouched
	assert.Equal(t,
		"https://localhost:8443/logs?secure=false",
		normalizeClickHouseDSN("https://localhost:8443/logs?secure=false"),
	)

	// non-https schemes are returned verbatim
	assert.Equal(t, "clickhouse://localhost:9000/logs", normalizeClickHouseDSN("clickhouse://localhost:9000/logs"))
	assert.Equal(t, "tcp://localhost:9000/logs", normalizeClickHouseDSN("tcp://localhost:9000/logs"))
}

func TestChooseDBRejectsClickHouseForMainDatabase(t *testing.T) {
	original, had := os.LookupEnv("SQL_DSN")
	t.Cleanup(func() {
		if had {
			require.NoError(t, os.Setenv("SQL_DSN", original))
		} else {
			require.NoError(t, os.Unsetenv("SQL_DSN"))
		}
	})
	require.NoError(t, os.Setenv("SQL_DSN", "clickhouse://default:pass@localhost:9000/logs"))

	db, dbType, err := chooseDB("SQL_DSN", false)
	require.Error(t, err)
	assert.Nil(t, db)
	assert.Equal(t, common.DatabaseType(""), dbType)
	assert.Contains(t, err.Error(), "does not support ClickHouse")
}

func TestClickHouseLogTTLExpression(t *testing.T) {
	assert.Equal(t, "", clickHouseLogTTLExpression(0))
	assert.Equal(t, "", clickHouseLogTTLExpression(-5))
	assert.Equal(t, "toDateTime(created_at) + INTERVAL 30 DAY DELETE", clickHouseLogTTLExpression(30))
}

func TestClickHouseLogTTLClause(t *testing.T) {
	assert.Equal(t, "", clickHouseLogTTLClause(0))
	assert.Equal(t, "\nTTL toDateTime(created_at) + INTERVAL 7 DAY DELETE", clickHouseLogTTLClause(7))
}

func TestClickHouseLogCreateTableSQL(t *testing.T) {
	withoutTTL := clickHouseLogCreateTableSQL(0)
	assert.Contains(t, withoutTTL, "CREATE TABLE IF NOT EXISTS logs")
	assert.Contains(t, withoutTTL, "ENGINE = MergeTree()")
	assert.Contains(t, withoutTTL, "PARTITION BY toYYYYMM(toDateTime(created_at))")
	assert.Contains(t, withoutTTL, "ORDER BY (created_at, request_id)")
	assert.NotContains(t, withoutTTL, "TTL ")

	withTTL := clickHouseLogCreateTableSQL(30)
	assert.Contains(t, withTTL, "ORDER BY (created_at, request_id)")
	assert.Contains(t, withTTL, "TTL toDateTime(created_at) + INTERVAL 30 DAY DELETE")
}

func TestClickHouseCreateTableHasTTL(t *testing.T) {
	assert.True(t, clickHouseCreateTableHasTTL("CREATE TABLE logs (...)\nTTL toDateTime(created_at) + INTERVAL 30 DAY DELETE"))
	assert.True(t, clickHouseCreateTableHasTTL("CREATE TABLE logs (...) TTL toDateTime(created_at)"))
	assert.False(t, clickHouseCreateTableHasTTL("CREATE TABLE logs (...)\nORDER BY (created_at, request_id)"))
}

func TestClickHouseLogOrder(t *testing.T) {
	assert.Equal(t, "created_at desc, request_id desc", clickHouseLogOrder(""))
	assert.Equal(t, "logs.created_at desc, logs.request_id desc", clickHouseLogOrder("logs."))
}

func TestBuildLogLikeConditionUsesStandardEscape(t *testing.T) {
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		common.SetLogDatabaseType(originalLogDatabaseType)
	})
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	condition, pattern, err := buildLogLikeCondition("logs.model_name", "gpt_4%")

	require.NoError(t, err)
	assert.Equal(t, "logs.model_name LIKE ? ESCAPE '!'", condition)
	assert.Equal(t, "gpt!_4%", pattern)
}

func TestBuildLogLikeConditionUsesClickHouseEscaping(t *testing.T) {
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		common.SetLogDatabaseType(originalLogDatabaseType)
	})
	common.SetLogDatabaseType(common.DatabaseTypeClickHouse)

	condition, pattern, err := buildLogLikeCondition("logs.model_name", `gpt_4\mini%`)

	require.NoError(t, err)
	assert.Equal(t, "logs.model_name LIKE ?", condition)
	assert.Equal(t, `gpt\_4\\mini%`, pattern)
}

func TestEnsureLogRequestId(t *testing.T) {
	empty := &Log{}
	ensureLogRequestId(empty)
	assert.NotEmpty(t, empty.RequestId, "empty request id should be backfilled")

	existing := &Log{RequestId: "fixed-request-id"}
	ensureLogRequestId(existing)
	assert.Equal(t, "fixed-request-id", existing.RequestId, "existing request id must be preserved")

	assert.NotPanics(t, func() { ensureLogRequestId(nil) })
}

func TestAssignDisplayLogIds(t *testing.T) {
	logs := []*Log{{}, {}, {}}

	assignDisplayLogIds(logs, 0)
	assert.Equal(t, []int{1, 2, 3}, []int{logs[0].Id, logs[1].Id, logs[2].Id})

	assignDisplayLogIds(logs, 20)
	assert.Equal(t, []int{21, 22, 23}, []int{logs[0].Id, logs[1].Id, logs[2].Id})

	assert.NotPanics(t, func() { assignDisplayLogIds(nil, 0) })
}

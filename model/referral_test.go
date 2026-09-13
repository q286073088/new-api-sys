package model

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The affected order schema from v1.0.0-rc.36. Its User schema is identical
// to this checkout; keeping that schema tests existing balances and indexes.
type releasedReferralTopUp struct {
	Id              int
	UserId          int `gorm:"index"`
	Amount          int64
	Money           float64
	TradeNo         string `gorm:"unique;type:varchar(255);index"`
	PaymentMethod   string `gorm:"type:varchar(50)"`
	PaymentProvider string `gorm:"type:varchar(50);default:''"`
	CreateTime      int64
	CompleteTime    int64
	Status          string
}

func (releasedReferralTopUp) TableName() string { return "top_ups" }

func setupReferralDatabase(t *testing.T, dialect string, released bool) {
	t.Helper()
	dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
	previousPath := common.SQLitePath
	if dialect == "sqlite" {
		dsn = "local"
		common.SQLitePath = filepath.Join(t.TempDir(), "referrals.db")
	}
	if dsn == "" {
		t.Skip("set TEST_" + strings.ToUpper(dialect) + "_DSN to run the real database matrix")
	}
	t.Setenv("REFERRAL_TEST_DSN", dsn)
	db, kind, err := chooseDB("REFERRAL_TEST_DSN", false)
	require.NoError(t, err)
	// Refuse to touch an existing application database, even if TEST_*_DSN
	// was accidentally pointed at one. All tables removed below are ours.
	tables, err := db.Migrator().GetTables()
	require.NoError(t, err)
	require.Empty(t, tables, "referral tests require an empty disposable database")
	sqlDB, err := db.DB()
	require.NoError(t, err)
	if dialect == "sqlite" {
		sqlDB.SetMaxOpenConns(1)
	}
	logDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "separate-logs.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, logDB.AutoMigrate(&Log{}))
	oldDB, oldLogDB := DB, LOG_DB
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	oldRedis, oldQuotaUnit := common.RedisEnabled, common.QuotaPerUnit
	oldPayment := *operation_setting.GetPaymentSetting()
	oldPrice, oldWaffo, oldPancake := operation_setting.Price, setting.WaffoUnitPrice, setting.WaffoPancakeUnitPrice
	oldReferral := setting.GetReferralSetting()
	DB, LOG_DB = db, logDB
	common.SetDatabaseTypes(kind, common.DatabaseTypeSQLite)
	initCol()
	common.RedisEnabled, common.QuotaPerUnit = false, 1000
	operation_setting.Price, setting.WaffoUnitPrice, setting.WaffoPancakeUnitPrice = 1, 1, 1
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	require.NoError(t, setting.UpdateReferralSetting(`{"enabled":true,"level1_percent":5,"level2_percent":2,"delay_days":3}`))
	t.Cleanup(func() {
		createdTables, tableErr := db.Migrator().GetTables()
		if assert.NoError(t, tableErr) {
			for _, table := range createdTables {
				if table != "sqlite_sequence" {
					assert.NoError(t, db.Migrator().DropTable(table))
				}
			}
		}
		assert.NoError(t, sqlDB.Close())
		logSQL, logErr := logDB.DB()
		if assert.NoError(t, logErr) {
			assert.NoError(t, logSQL.Close())
		}
		DB, LOG_DB = oldDB, oldLogDB
		common.SetDatabaseTypes(oldMainType, oldLogType)
		initCol()
		common.RedisEnabled, common.QuotaPerUnit, common.SQLitePath = oldRedis, oldQuotaUnit, previousPath
		*operation_setting.GetPaymentSetting() = oldPayment
		operation_setting.Price, setting.WaffoUnitPrice, setting.WaffoPancakeUnitPrice = oldPrice, oldWaffo, oldPancake
		assert.NoError(t, setting.UpdateReferralSetting(common.GetJsonString(oldReferral)))
	})
	if released {
		require.NoError(t, db.AutoMigrate(&User{}, &releasedReferralTopUp{}, &Option{}))
	} else {
		require.NoError(t, migrateDB())
		require.NoError(t, migrateDB())
	}
	versionQuery := "SELECT version()"
	if dialect == "sqlite" {
		versionQuery = "SELECT sqlite_version()"
	}
	var version string
	require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
	t.Logf("%s: %s; separate SQLite log database", dialect, version)
}

func referralUsers(t *testing.T) []User {
	t.Helper()
	users := []User{
		{Id: 1, Username: "grandparent", AffCode: "root", AffQuota: 100, AffHistoryQuota: 100},
		{Id: 2, Username: "parent", AffCode: "parent", InviterId: 1},
		{Id: 3, Username: "buyer", AffCode: "buyer", InviterId: 2},
		{Id: 4, Username: "unrelated", AffCode: "other"},
	}
	for i := range users {
		require.NoError(t, DB.Create(&users[i]).Error)
	}
	return users
}

func TestReferralManualQuotaAdd(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			setupReferralDatabase(t, dialect, false)
			users := referralUsers(t)
			for i := range 3 {
				require.NoError(t, DB.Model(&users[i]).Update("email", users[i].Username+"@example.test").Error)
			}
			for _, tc := range []struct {
				name, mode string
				value      int
				enabled    bool
				delay      int
				wantReward bool
			}{
				{"immediate", "add", 12345, true, 0, true},
				{"delayed", "add", 20000, true, 3, true},
				{"disabled", "add", 1000, false, 0, false},
				{"subtract", "subtract", 1000, true, 0, false},
				{"override", "override", 40000, true, 0, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					require.NoError(t, setting.UpdateReferralSetting(fmt.Sprintf(`{"enabled":%t,"level1_percent":5,"level2_percent":2,"delay_days":%d}`, tc.enabled, tc.delay)))
					var beforeRewards, beforeOrders int64
					require.NoError(t, DB.Model(&ReferralReward{}).Count(&beforeRewards).Error)
					require.NoError(t, DB.Model(&TopUp{}).Count(&beforeOrders).Error)
					adjustment, err := AdjustUserQuota(3, common.RoleRootUser, tc.mode, tc.value)
					require.NoError(t, err)
					var buyer User
					require.NoError(t, DB.First(&buyer, 3).Error)
					assert.Equal(t, adjustment.After, buyer.Quota)
					if tc.mode == "add" {
						assert.Equal(t, tc.value, adjustment.After-adjustment.Before, "manual credit must be applied once")
					}
					var rewards []ReferralReward
					require.NoError(t, DB.Where("id > ?", beforeRewards).Order("level").Find(&rewards).Error)
					var orderCount int64
					require.NoError(t, DB.Model(&TopUp{}).Count(&orderCount).Error)
					if tc.mode != "add" {
						assert.Equal(t, beforeOrders, orderCount)
						assert.Empty(t, rewards)
						return
					}
					assert.Equal(t, beforeOrders+1, orderCount)
					var order TopUp
					require.NoError(t, DB.Last(&order).Error)
					assert.Equal(t, "admin", order.PaymentProvider)
					assert.Equal(t, "admin", order.PaymentMethod)
					assert.Equal(t, common.TopUpStatusSuccess, order.Status)
					assert.EqualValues(t, tc.value, order.Amount)
					require.NotNil(t, order.ReferralBaseQuota)
					assert.Equal(t, tc.value, *order.ReferralBaseQuota)
					if !tc.wantReward {
						assert.Empty(t, rewards)
						return
					}
					require.Len(t, rewards, 2)
					assert.Equal(t, tc.value*5/100, rewards[0].Quota)
					assert.Equal(t, tc.value*2/100, rewards[1].Quota)
					assert.Equal(t, 2, rewards[0].UserID)
					assert.Equal(t, 1, rewards[1].UserID)
					for _, reward := range rewards {
						assert.Equal(t, order.Id, reward.TopUpID)
						assert.Equal(t, order.CompleteTime+int64(tc.delay)*86400, reward.AvailableAt)
						if tc.delay == 0 {
							assert.Equal(t, ReferralRewardSettled, reward.Status)
						} else {
							assert.Equal(t, ReferralRewardPending, reward.Status)
						}
					}
					if tc.delay > 0 {
						count, err := SettleDueReferralRewards(t.Context(), rewards[0].AvailableAt-1)
						require.NoError(t, err)
						assert.Zero(t, count)
						count, err = SettleDueReferralRewards(t.Context(), rewards[0].AvailableAt)
						require.NoError(t, err)
						assert.Equal(t, 2, count)
					}
					require.NoError(t, ManualCompleteTopUp(order.TradeNo, ""))
					count, err := SettleDueReferralRewards(t.Context(), rewards[0].AvailableAt+1)
					require.NoError(t, err)
					assert.Zero(t, count, "settlement and completing the manual order must not duplicate rewards")
					var emails int64
					require.NoError(t, DB.Model(&EmailNotification{}).Where("event_key IN ?", []string{
						fmt.Sprintf("topup:%d", order.Id), fmt.Sprintf("referral:%d", rewards[0].ID), fmt.Sprintf("referral:%d", rewards[1].ID),
					}).Count(&emails).Error)
					assert.EqualValues(t, 3, emails)
				})
			}
			invitees, _, err := GetReferralInvitees(2, 0, &common.PageInfo{Page: 1, PageSize: 10})
			require.NoError(t, err)
			require.Len(t, invitees, 1)
			assert.Equal(t, 33345, invitees[0].TopUpQuota, "manual additions count at exact quota precision")
			require.NoError(t, DB.First(&users[0], 1).Error)
			require.NoError(t, DB.First(&users[1], 2).Error)
			assert.Equal(t, 746, users[0].AffQuota)
			assert.Equal(t, 1617, users[1].AffQuota)
			for _, failedTable := range []string{"top_ups", "email_notifications", "referral_rewards"} {
				t.Run("rollback_"+failedTable, func(t *testing.T) {
					require.NoError(t, DB.Callback().Create().Before("gorm:create").Register("test:manual_credit_failure", func(tx *gorm.DB) {
						if tx.Statement.Table == failedTable {
							tx.AddError(errors.New("manual credit persistence unavailable"))
						}
					}))
					t.Cleanup(func() { assert.NoError(t, DB.Callback().Create().Remove("test:manual_credit_failure")) })
					adjustment, err := AdjustUserQuota(3, common.RoleRootUser, "add", 1000)
					require.Error(t, err)
					assert.Nil(t, adjustment)
					var buyer, parent User
					require.NoError(t, DB.First(&buyer, 3).Error)
					require.NoError(t, DB.First(&parent, 2).Error)
					assert.Equal(t, 40000, buyer.Quota)
					assert.Equal(t, 1617, parent.AffQuota)
					for _, expected := range []struct {
						model any
						count int64
					}{{&TopUp{}, 3}, {&ReferralReward{}, 4}, {&EmailNotification{}, 7}} {
						var count int64
						require.NoError(t, DB.Model(expected.model).Count(&count).Error)
						assert.Equal(t, expected.count, count)
					}
				})
			}
		})
	}
}

func TestReferralRebatesDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			setupReferralDatabase(t, dialect, false)
			users := referralUsers(t)
			topUp := TopUp{UserId: 3, Amount: 100, Money: 90, TradeNo: "discounted-order", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending}
			require.NoError(t, topUp.Insert())
			operation_setting.Price = 2 // Checkout price must remain frozen.
			already, err := RechargeEpay(topUp.TradeNo, "alipay", "127.0.0.1")
			require.NoError(t, err)
			assert.False(t, already)
			already, err = RechargeEpay(topUp.TradeNo, "alipay", "127.0.0.1")
			require.NoError(t, err)
			assert.True(t, already)
			var rewards []ReferralReward
			require.NoError(t, DB.Order("level").Find(&rewards).Error)
			require.Len(t, rewards, 2)
			assert.Equal(t, 4500, rewards[0].Quota)
			assert.Equal(t, 1800, rewards[1].Quota)
			assert.Equal(t, 90000, rewards[0].BaseQuota)
			assert.Equal(t, 2, rewards[1].DirectInviteeID)
			assert.Equal(t, 3, rewards[1].InviteeID)
			assert.Equal(t, rewards[0].CreatedAt+3*86400, rewards[0].AvailableAt)
			var buyer User
			require.NoError(t, DB.First(&buyer, 3).Error)
			assert.Equal(t, 100000, buyer.Quota)
			summary, err := GetReferralSummary(1)
			require.NoError(t, err)
			assert.EqualValues(t, 1, summary.DirectCount, "registration bonuses need not be enabled for invitations to be counted")
			assert.EqualValues(t, 1, summary.TeamCount)
			assert.EqualValues(t, 1800, summary.PendingQuota)
			assert.Equal(t, 100, summary.AvailableQuota)
			page := &common.PageInfo{Page: 1, PageSize: 10}
			invitees, total, err := GetReferralInvitees(1, 0, page)
			require.NoError(t, err)
			require.Len(t, invitees, 1)
			assert.EqualValues(t, 1, total)
			assert.EqualValues(t, 1800, invitees[0].TeamRewardQuota)
			team, _, err := GetReferralInvitees(1, 2, page)
			require.NoError(t, err)
			require.Len(t, team, 1)
			assert.Equal(t, 100000, team[0].TopUpQuota)
			assert.EqualValues(t, 1800, team[0].TeamRewardQuota)
			_, _, err = GetReferralInvitees(4, 2, page)
			assert.ErrorIs(t, err, ErrReferralNotOwned)
			_, _, err = GetReferralInvitees(1, 3, page)
			assert.ErrorIs(t, err, ErrReferralNotOwned, "only one team level can be expanded")
			details, total, err := GetReferralRewards(4, page)
			require.NoError(t, err)
			assert.Empty(t, details)
			assert.Zero(t, total)
			count, err := SettleDueReferralRewards(context.Background(), rewards[0].AvailableAt-1)
			require.NoError(t, err)
			assert.Zero(t, count)
			require.NoError(t, setting.UpdateReferralSetting(`{"enabled":false,"level1_percent":90,"level2_percent":0,"delay_days":0}`))
			count, err = SettleDueReferralRewards(context.Background(), rewards[0].AvailableAt)
			require.NoError(t, err)
			assert.Equal(t, 2, count)
			count, err = SettleDueReferralRewards(context.Background(), rewards[0].AvailableAt+86400)
			require.NoError(t, err)
			assert.Zero(t, count)
			summary, err = GetReferralSummary(1)
			require.NoError(t, err)
			assert.Zero(t, summary.PendingQuota)
			assert.Equal(t, 1900, summary.AvailableQuota)
			assert.Equal(t, 1900, summary.TotalEarned)
			require.NoError(t, users[0].TransferAffQuotaToQuota(1000))
			require.NoError(t, DB.First(&users[0], 1).Error)
			assert.Equal(t, 900, users[0].AffQuota)
			assert.Equal(t, 1000, users[0].Quota)
			var rewardCount int64
			require.NoError(t, DB.Model(&ReferralReward{}).Count(&rewardCount).Error)
			assert.EqualValues(t, 2, rewardCount, "transfers do not generate rebates")
			var logCount int64
			require.NoError(t, LOG_DB.Model(&Log{}).Where("type = ?", LogTypeTopup).Count(&logCount).Error)
			assert.EqualValues(t, 1, logCount, "a repeated callback does not repeat the top-up log")
		})
	}
}

func TestReferralPaymentPathsAndBounds(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			setupReferralDatabase(t, dialect, false)
			referralUsers(t)
			require.NoError(t, setting.UpdateReferralSetting(`{"enabled":true,"level1_percent":5,"level2_percent":0,"delay_days":0}`))
			for _, provider := range []string{PaymentProviderStripe, PaymentProviderCreem, PaymentProviderWaffo, PaymentProviderWaffoPancake, "manual"} {
				t.Run(provider, func(t *testing.T) {
					topUp := TopUp{UserId: 3, Amount: 100, Money: 90, TradeNo: provider, PaymentProvider: provider, Status: common.TopUpStatusPending}
					if provider == PaymentProviderCreem {
						topUp.Amount = 100000
					}
					if provider == "manual" {
						topUp.PaymentProvider = PaymentProviderCreem
						topUp.Amount = 90000
					}
					require.NoError(t, topUp.Insert())
					payment := &TopUpPayment{PaidAmount: 9000, ListAmount: 10000}
					var err error
					switch provider {
					case PaymentProviderStripe:
						err = Recharge(topUp.TradeNo, "", "", payment)
					case PaymentProviderCreem:
						err = RechargeCreem(topUp.TradeNo, "", "", "", payment)
					case PaymentProviderWaffo:
						err = RechargeWaffo(topUp.TradeNo, "")
					case PaymentProviderWaffoPancake:
						err = RechargeWaffoPancake(topUp.TradeNo)
					default:
						err = ManualCompleteTopUp(topUp.TradeNo, "")
					}
					require.NoError(t, err)
					require.NoError(t, ManualCompleteTopUp(topUp.TradeNo, ""))
					var rewards []ReferralReward
					require.NoError(t, DB.Where("top_up_id = ?", topUp.Id).Find(&rewards).Error)
					require.Len(t, rewards, 1)
					assert.Equal(t, 4500, rewards[0].Quota)
					assert.Equal(t, ReferralRewardSettled, rewards[0].Status)
				})
			}
			require.NoError(t, setting.UpdateReferralSetting(`{"enabled":true,"level1_percent":5,"level2_percent":2,"delay_days":3}`))
			topUp := TopUp{UserId: 3, Amount: 100, Money: 100, TradeNo: "blocked-recipient", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending}
			require.NoError(t, topUp.Insert())
			_, err := RechargeEpay(topUp.TradeNo, "", "")
			require.NoError(t, err)
			require.NoError(t, DB.Model(&User{}).Where("id = 2").Update("aff_quota", common.MaxWalletQuota).Error)
			var reward ReferralReward
			require.NoError(t, DB.Where("top_up_id = ? AND level = 1", topUp.Id).First(&reward).Error)
			count, err := SettleDueReferralRewards(context.Background(), reward.AvailableAt)
			require.NoError(t, err)
			assert.Equal(t, 1, count, "one full balance must not block the other recipient")
			require.NoError(t, DB.First(&reward, reward.ID).Error)
			assert.Equal(t, ReferralRewardPending, reward.Status)
			require.NoError(t, DB.Model(&User{}).Where("id = 2").Update("aff_quota", 0).Error)
			count, err = SettleDueReferralRewards(context.Background(), reward.AvailableAt)
			require.NoError(t, err)
			assert.Equal(t, 1, count)
			require.NoError(t, DB.Model(&User{}).Where("id = 2").Update("quota", common.MaxWalletQuota).Error)
			parent := User{Id: 2}
			assert.ErrorIs(t, parent.TransferAffQuotaToQuota(1000), ErrTopUpQuotaLimitExceeded)
			require.NoError(t, DB.First(&parent, 2).Error)
			assert.Equal(t, 5000, parent.AffQuota)
			assert.Equal(t, common.MaxWalletQuota, parent.Quota)
		})
	}
}

func TestReferralConcurrentSettlementAndCycles(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			setupReferralDatabase(t, dialect, false)
			referralUsers(t)
			topUp := TopUp{UserId: 3, Amount: 100, Money: 100, TradeNo: "concurrent", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending}
			require.NoError(t, topUp.Insert())
			start := make(chan struct{})
			errors := make(chan error, 2)
			var group sync.WaitGroup
			for range 2 {
				group.Go(func() {
					<-start
					_, err := RechargeEpay(topUp.TradeNo, "", "")
					errors <- err
				})
			}
			close(start)
			group.Wait()
			for range 2 {
				require.NoError(t, <-errors)
			}
			var reward ReferralReward
			require.NoError(t, DB.First(&reward).Error)
			for range 2 {
				group.Go(func() {
					_, err := SettleDueReferralRewards(context.Background(), reward.AvailableAt)
					errors <- err
				})
			}
			group.Wait()
			for range 2 {
				require.NoError(t, <-errors)
			}
			var parent User
			require.NoError(t, DB.First(&parent, 2).Error)
			assert.Equal(t, 5000, parent.AffQuota)
			var count int64
			require.NoError(t, DB.Model(&ReferralReward{}).Count(&count).Error)
			assert.EqualValues(t, 2, count)
			// A malformed old relationship must never pay the buyer themselves.
			require.NoError(t, DB.Model(&User{}).Where("id = 2").Update("inviter_id", 3).Error)
			topUp.Id, topUp.TradeNo, topUp.Status = 0, "cycle", common.TopUpStatusPending
			require.NoError(t, topUp.Insert())
			_, err := RechargeEpay(topUp.TradeNo, "", "")
			require.NoError(t, err)
			var cycleRewards []ReferralReward
			require.NoError(t, DB.Where("top_up_id = ?", topUp.Id).Find(&cycleRewards).Error)
			require.Len(t, cycleRewards, 1)
			assert.Equal(t, 2, cycleRewards[0].UserID)
		})
	}
}

func TestReferralReleasedSchemaMigration(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			setupReferralDatabase(t, dialect, true)
			referralUsers(t)
			oldOrder := releasedReferralTopUp{UserId: 3, Amount: 100, Money: 100, TradeNo: "released-order", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusSuccess}
			require.NoError(t, DB.Create(&oldOrder).Error)
			require.NoError(t, migrateDB())
			require.NoError(t, migrateDB())
			recorder := &migrationSQLRecorder{}
			require.NoError(t, DB.Session(&gorm.Session{Logger: recorder}).AutoMigrate(&TopUp{}, &ReferralReward{}))
			assert.Empty(t, recorder.schemaMutations(), "restarting must not repeat DDL for referral tables")
			var order TopUp
			require.NoError(t, DB.First(&order, oldOrder.Id).Error)
			assert.Nil(t, order.ReferralBaseQuota)
			assert.Equal(t, common.TopUpStatusSuccess, order.Status)
			assert.EqualValues(t, 100, order.Amount)
			already, err := RechargeEpay(order.TradeNo, "", "")
			require.NoError(t, err)
			assert.True(t, already)
			var rewardCount int64
			require.NoError(t, DB.Model(&ReferralReward{}).Count(&rewardCount).Error)
			assert.Zero(t, rewardCount, "historical payments must not be rewarded again")
			var root User
			require.NoError(t, DB.First(&root, 1).Error)
			assert.Equal(t, 100, root.AffQuota)
			assert.Equal(t, 100, root.AffHistoryQuota)
			duplicate := order
			duplicate.Id = 0
			assert.Error(t, DB.Create(&duplicate).Error, "existing trade number uniqueness survives migration")
			pending := releasedReferralTopUp{UserId: 3, Amount: 100, Money: 100, TradeNo: "released-pending", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending}
			require.NoError(t, DB.Create(&pending).Error)
			_, err = RechargeEpay(pending.TradeNo, "", "")
			require.NoError(t, err)
			var reward ReferralReward
			require.NoError(t, DB.First(&reward).Error)
			reward.ID = 0
			assert.Error(t, DB.Create(&reward).Error, "one payment can reward each level only once")
			invitees, _, err := GetReferralInvitees(2, 0, &common.PageInfo{Page: 1, PageSize: 10})
			require.NoError(t, err)
			require.Len(t, invitees, 1)
			assert.Equal(t, 200000, invitees[0].TopUpQuota)
			for _, historical := range []TopUp{
				{UserId: 3, Amount: 1, Money: 1, TradeNo: "legacy-epay", PaymentMethod: "alipay", Status: common.TopUpStatusSuccess},
				{UserId: 3, Amount: 2, Money: 2.5, TradeNo: "legacy-stripe", PaymentMethod: PaymentMethodStripe, Status: common.TopUpStatusSuccess},
				{UserId: 3, Amount: 3000, Money: 3, TradeNo: "legacy-creem", PaymentMethod: PaymentMethodCreem, Status: common.TopUpStatusSuccess},
				{UserId: 3, Amount: 0, Money: 100, TradeNo: "subscription", PaymentMethod: PaymentMethodStripe, Status: common.TopUpStatusSuccess},
				{UserId: 3, Amount: 999, Money: 999, TradeNo: "not-paid", PaymentMethod: "alipay", Status: common.TopUpStatusPending},
			} {
				require.NoError(t, DB.Create(&historical).Error)
			}
			invitees, _, err = GetReferralInvitees(2, 0, &common.PageInfo{Page: 1, PageSize: 10})
			require.NoError(t, err)
			assert.Equal(t, 206500, invitees[0].TopUpQuota)
		})
	}
}

func TestReferralSettingValidation(t *testing.T) {
	for _, input := range []string{
		`null`, `{"level1_percent":-1}`, `{"level1_percent":101}`, `{"level2_percent":0.001}`,
		`{"level1_percent":99,"level2_percent":2}`, `{"delay_days":-1}`, `{"delay_days":366}`, `{"delay_days":1.5}`,
	} {
		t.Run(input, func(t *testing.T) {
			_, err := setting.ParseReferralSetting(input)
			assert.Error(t, err)
		})
	}
	for _, input := range []string{`{"level1_percent":5,"level2_percent":2,"delay_days":3}`, `{"level1_percent":99.99,"level2_percent":0.01,"delay_days":0}`} {
		_, err := setting.ParseReferralSetting(input)
		require.NoError(t, err, fmt.Sprintf("valid setting: %s", input))
	}
}

func TestReferralPaymentRollbackAndEligibility(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			setupReferralDatabase(t, dialect, false)
			referralUsers(t)
			order := TopUp{UserId: 3, Amount: 100, Money: 100, TradeNo: "invalid-receipt", PaymentProvider: PaymentProviderStripe, Status: common.TopUpStatusPending}
			require.NoError(t, order.Insert())
			for _, payment := range []TopUpPayment{{PaidAmount: -1, ListAmount: 100}, {PaidAmount: 1, ListAmount: 0}, {PaidAmount: int64(common.MaxWalletQuota), ListAmount: 1}} {
				assert.Error(t, Recharge(order.TradeNo, "", "", &payment))
			}
			var buyer User
			require.NoError(t, DB.First(&buyer, 3).Error)
			assert.Zero(t, buyer.Quota)
			assert.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo(order.TradeNo).Status)
			require.NoError(t, Recharge(order.TradeNo, "", "", &TopUpPayment{PaidAmount: 0, ListAmount: 100}))
			var count int64
			require.NoError(t, DB.Model(&ReferralReward{}).Count(&count).Error)
			assert.Zero(t, count, "a fully discounted payment must not earn a rebate")

			order = TopUp{UserId: 3, Amount: 100, Money: 100, TradeNo: "ledger-unavailable", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending}
			require.NoError(t, order.Insert())
			require.NoError(t, DB.Migrator().DropTable(&ReferralReward{}))
			_, err := RechargeEpay(order.TradeNo, "", "")
			require.Error(t, err)
			require.NoError(t, DB.First(&buyer, 3).Error)
			assert.Equal(t, 100000, buyer.Quota, "only the earlier completed payment remains credited")
			assert.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo(order.TradeNo).Status)
			require.NoError(t, DB.AutoMigrate(&ReferralReward{}))
			_, err = RechargeEpay(order.TradeNo, "", "")
			require.NoError(t, err)
			var reward ReferralReward
			require.NoError(t, DB.Where("top_up_id = ?", order.Id).First(&reward).Error)
			require.NoError(t, DB.Model(&order).Update("status", common.TopUpStatusFailed).Error)
			settled, err := SettleDueReferralRewards(context.Background(), reward.AvailableAt)
			require.NoError(t, err)
			assert.Zero(t, settled)
			require.NoError(t, DB.Model(&ReferralReward{}).Where("status = ?", ReferralRewardCancelled).Count(&count).Error)
			assert.EqualValues(t, 2, count)

			third := User{Id: 5, Username: "third-level", AffCode: "third", InviterId: 3}
			require.NoError(t, DB.Create(&third).Error)
			thirdOrder := TopUp{UserId: 5, Amount: 100, Money: 100, TradeNo: "third-level", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending}
			require.NoError(t, thirdOrder.Insert())
			_, err = RechargeEpay(thirdOrder.TradeNo, "", "")
			require.NoError(t, err)
			require.NoError(t, DB.Model(&ReferralReward{}).Where("top_up_id = ? AND user_id = ?", thirdOrder.Id, 1).Count(&count).Error)
			assert.Zero(t, count, "rewards stop after two ancestors")
			require.NoError(t, setting.UpdateReferralSetting(`{"enabled":false,"level1_percent":5,"level2_percent":2,"delay_days":3}`))
			offOrder := TopUp{UserId: 3, Amount: 100, Money: 100, TradeNo: "rebates-disabled", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending}
			require.NoError(t, offOrder.Insert())
			_, err = RechargeEpay(offOrder.TradeNo, "", "")
			require.NoError(t, err)
			require.NoError(t, DB.Model(&ReferralReward{}).Where("top_up_id = ?", offOrder.Id).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

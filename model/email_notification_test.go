package model

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotificationOutboxDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		for _, released := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/upgrade_%t", dialect, released), func(t *testing.T) {
				setupReferralDatabase(t, dialect, released)
				users := referralUsers(t)
				for _, user := range users[:3] {
					require.NoError(t, DB.Model(&user).Update("email", user.Username+"@example.test").Error)
				}
				if released {
					require.NoError(t, DB.Create(&releasedReferralTopUp{UserId: 3, Amount: 1, TradeNo: "historical", Status: common.TopUpStatusSuccess}).Error)
					require.NoError(t, LOG_DB.Migrator().DropColumn(&Log{}, "HiddenForUser"))
					require.NoError(t, LOG_DB.Omit("HiddenForUser").Create(&Log{UserId: 3, Type: LogTypeTopup, Content: "historical log"}).Error)
				}
				for range 2 {
					require.NoError(t, migrateDB())
					require.NoError(t, migrateLOGDB())
				}
				var count int64
				require.NoError(t, DB.Model(&EmailNotification{}).Count(&count).Error)
				assert.Zero(t, count, "upgrades must not email historical payments")
				if released {
					logs, total, err := GetUserLogs(3, LogTypeUnknown, 0, 0, "", "", 0, 10, "", "", "")
					require.NoError(t, err)
					assert.EqualValues(t, 1, total)
					require.Len(t, logs, 1)
					assert.Equal(t, "historical log", logs[0].Content)
				}
				ctx := context.Background()
				require.NoError(t, MarkAnnouncementViewed(ctx, 3, "id:announcement"))
				require.NoError(t, MarkAnnouncementViewed(ctx, 3, "id:announcement"))
				keys, err := GetAnnouncementViews(ctx, 3)
				require.NoError(t, err)
				assert.Equal(t, []string{"id:announcement"}, keys)
				keys, err = GetAnnouncementViews(ctx, 2)
				require.NoError(t, err)
				assert.Empty(t, keys)

				order := TopUp{UserId: 3, Amount: 100, Money: 100, TradeNo: "notified-payment", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending}
				require.NoError(t, order.Insert())
				_, err = RechargeEpay(order.TradeNo, "", "")
				require.NoError(t, err)
				_, err = RechargeEpay(order.TradeNo, "", "")
				require.NoError(t, err)
				var emails []EmailNotification
				require.NoError(t, DB.Find(&emails).Error)
				require.Len(t, emails, 1)
				assert.Equal(t, "buyer@example.test", emails[0].Email)
				assert.Contains(t, emails[0].Content, order.TradeNo)
				var reward ReferralReward
				require.NoError(t, DB.First(&reward).Error)
				settled, err := SettleDueReferralRewards(ctx, reward.AvailableAt-1)
				require.NoError(t, err)
				assert.Zero(t, settled)
				settled, err = SettleDueReferralRewards(ctx, reward.AvailableAt)
				require.NoError(t, err)
				assert.Equal(t, 2, settled)
				_, err = SettleDueReferralRewards(ctx, reward.AvailableAt)
				require.NoError(t, err)
				require.NoError(t, DB.Find(&emails).Error)
				require.Len(t, emails, 3)
				now := common.GetTimestamp()
				sent := make(map[string]int)
				summary, err := DispatchEmailNotifications(ctx, now, func(subject, receiver, content string) error {
					sent[receiver]++
					if receiver == "parent@example.test" {
						return errors.New("SMTP unavailable")
					}
					return nil
				})
				require.Error(t, err)
				assert.Equal(t, EmailDeliverySummary{Sent: 2, Failed: 1}, summary)
				var parent User
				require.NoError(t, DB.First(&parent, 2).Error)
				assert.Equal(t, 5000, parent.AffQuota, "SMTP failure cannot undo settlement")
				sender := func(_, receiver, _ string) error { sent[receiver]++; return nil }
				summary, err = DispatchEmailNotifications(ctx, now, sender)
				require.NoError(t, err)
				assert.Zero(t, summary.Sent)
				summary, err = DispatchEmailNotifications(ctx, now+300, sender)
				require.NoError(t, err)
				assert.Equal(t, 1, summary.Sent)
				assert.Equal(t, map[string]int{"buyer@example.test": 1, "parent@example.test": 2, "grandparent@example.test": 1}, sent)

				require.NoError(t, setting.UpdateReferralSetting(`{"enabled":true,"level1_percent":5,"level2_percent":2,"delay_days":0}`))
				immediate := TopUp{UserId: 3, Amount: 100, Money: 100, TradeNo: "immediate-payment", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending}
				require.NoError(t, immediate.Insert())
				_, err = RechargeEpay(immediate.TradeNo, "", "")
				require.NoError(t, err)
				require.NoError(t, DB.Model(&EmailNotification{}).Count(&count).Error)
				assert.EqualValues(t, 6, count)
			})
		}
	}
}

func TestEmailDeliveryClaimsAndRecipientChanges(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			setupReferralDatabase(t, dialect, false)
			users := referralUsers(t)
			require.NoError(t, DB.Model(&users[2]).Update("email", "buyer@example.test").Error)
			queued, err := QueueUserEmail(DB, "admin:one", 3, "Hello", "<script>alert('x')</script>\nWelcome")
			require.NoError(t, err)
			assert.True(t, queued)
			queued, err = QueueUserEmail(DB, "admin:one", 3, "Hello", "same request")
			require.NoError(t, err)
			assert.False(t, queued)
			queued, err = QueueUserEmail(DB, "admin:no-email", 4, "Hello", "Welcome")
			require.NoError(t, err)
			assert.False(t, queued)
			now, ctx := common.GetTimestamp()+3600, context.Background()
			summary, err := DispatchEmailNotifications(ctx, now, func(_, receiver, body string) error {
				assert.Equal(t, "buyer@example.test", receiver)
				assert.NotContains(t, body, "<script>")
				assert.Contains(t, body, "&lt;script&gt;")
				assert.Contains(t, body, "<br>")
				other, err := DispatchEmailNotifications(ctx, now, func(_, _, _ string) error { t.Error("another worker sent a claimed email"); return nil })
				require.NoError(t, err)
				assert.Zero(t, other.Sent)
				return nil
			})
			require.NoError(t, err)
			assert.Equal(t, 1, summary.Sent)
			_, err = QueueUserEmail(DB, "admin:old-address", 3, "Hello", "Welcome")
			require.NoError(t, err)
			require.NoError(t, DB.Model(&users[2]).Update("email", "changed@example.test").Error)
			summary, err = DispatchEmailNotifications(ctx, now, func(_, _, _ string) error { t.Error("email sent to old address"); return nil })
			require.NoError(t, err)
			assert.Equal(t, 1, summary.Skipped)
		})
	}
}

package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const (
	ReferralRewardPending   = "pending"
	ReferralRewardSettled   = "settled"
	ReferralRewardCancelled = "cancelled"
)

// ReferralReward is both the settlement queue and the immutable per-order
// record. Rates, beneficiaries and due dates are frozen when payment completes.
type ReferralReward struct {
	ID              int     `json:"id"`
	TopUpID         int     `json:"top_up_id" gorm:"uniqueIndex:idx_referral_order_level"`
	UserID          int     `json:"user_id" gorm:"index:idx_referral_user_direct"`
	InviteeID       int     `json:"invitee_id"`
	DirectInviteeID int     `json:"direct_invitee_id" gorm:"index:idx_referral_user_direct"`
	Level           int     `json:"level" gorm:"uniqueIndex:idx_referral_order_level"`
	BaseQuota       int     `json:"base_quota" gorm:"type:bigint"`
	Rate            float64 `json:"rate"`
	Quota           int     `json:"quota" gorm:"type:bigint"`
	Status          string  `json:"status" gorm:"type:varchar(16);index:idx_referral_due"`
	CreatedAt       int64   `json:"created_at"`
	AvailableAt     int64   `json:"available_at" gorm:"index:idx_referral_due"`
	SettledAt       int64   `json:"settled_at"`
}

// TopUpPayment uses the provider's original integer monetary units. A ratio
// avoids currency-specific decimal places (for example JPY versus USD).
type TopUpPayment struct {
	PaidAmount int64
	ListAmount int64
}

// snapshotReferralBase converts paid money using the gateway's unit price at
// checkout, before later price/discount changes. Stripe and Creem additionally
// apply the verified receipt's discount ratio when completing the payment.
func (topUp *TopUp) snapshotReferralBase() error {
	if topUp.ReferralBaseQuota != nil {
		if *topUp.ReferralBaseQuota < 0 || *topUp.ReferralBaseQuota > common.MaxWalletQuota {
			return ErrInvalidTopUpQuota
		}
		return nil
	}
	if math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) || common.QuotaPerUnit <= 0 {
		return ErrInvalidTopUpQuota
	}
	base := decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	unitPrice := 0.0
	switch topUp.PaymentProvider {
	case PaymentProviderEpay:
		unitPrice = operation_setting.Price
	case PaymentProviderWaffo:
		unitPrice = setting.WaffoUnitPrice
	case PaymentProviderWaffoPancake:
		unitPrice = setting.WaffoPancakeUnitPrice
	case PaymentProviderCreem:
		base = decimal.NewFromInt(topUp.Amount)
	case PaymentProviderStripe:
		// Amount is the undiscounted number of Stripe price units purchased.
	default:
		return nil // Subscription and balance-payment records are not top-ups.
	}
	if topUp.PaymentProvider == PaymentProviderEpay || topUp.PaymentProvider == PaymentProviderWaffo || topUp.PaymentProvider == PaymentProviderWaffoPancake {
		if math.IsNaN(unitPrice) || math.IsInf(unitPrice, 0) || unitPrice <= 0 ||
			math.IsNaN(topUp.Money) || math.IsInf(topUp.Money, 0) || topUp.Money < 0 {
			return ErrInvalidTopUpQuota
		}
		paidMoney := decimal.NewFromFloat(topUp.Money).Round(2)
		if topUp.PaymentProvider == PaymentProviderWaffo {
			switch strings.ToUpper(setting.WaffoCurrency) {
			case "IDR", "JPY", "KRW", "VND":
				paidMoney = decimal.NewFromFloat(topUp.Money).Round(0)
			}
		}
		base = paidMoney.
			Div(decimal.NewFromFloat(unitPrice)).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	}
	quota, err := common.WalletQuotaFromDecimalStrict(base.Truncate(0))
	if err != nil || quota < 0 {
		return ErrInvalidTopUpQuota
	}
	topUp.ReferralBaseQuota = &quota
	return nil
}

func (topUp *TopUp) applyReferralPayment(payment *TopUpPayment) error {
	if err := topUp.snapshotReferralBase(); err != nil {
		return err
	}
	if payment == nil || topUp.ReferralBaseQuota == nil {
		return nil
	}
	if payment.PaidAmount < 0 || payment.ListAmount <= 0 {
		return ErrInvalidTopUpQuota
	}
	base := decimal.NewFromInt(int64(*topUp.ReferralBaseQuota)).
		Mul(decimal.NewFromInt(payment.PaidAmount)).Div(decimal.NewFromInt(payment.ListAmount))
	quota, err := common.WalletQuotaFromDecimalStrict(base.Truncate(0))
	if err != nil || quota < 0 {
		return ErrInvalidTopUpQuota
	}
	topUp.ReferralBaseQuota = &quota
	return nil
}

func creditTopUpWithReferral(tx *gorm.DB, topUp *TopUp, creditedQuota int, updates map[string]any) error {
	if err := creditTopUpQuota(tx, topUp.UserId, creditedQuota, updates); err != nil {
		return err
	}
	if _, err := QueueUserEmail(tx, fmt.Sprintf("topup:%d", topUp.Id), topUp.UserId,
		"充值成功", fmt.Sprintf("您的充值已成功入账。\n订单号：%s\n到账额度：%s", topUp.TradeNo, logger.LogQuota(creditedQuota))); err != nil {
		return err
	}
	if topUp.ReferralBaseQuota == nil {
		if err := topUp.snapshotReferralBase(); err != nil {
			return err
		}
		if err := tx.Model(topUp).Update("referral_base_quota", topUp.ReferralBaseQuota).Error; err != nil {
			return err
		}
	}
	settings := setting.GetReferralSetting()
	if !settings.Enabled || !operation_setting.IsPaymentComplianceConfirmed() || topUp.ReferralBaseQuota == nil || *topUp.ReferralBaseQuota <= 0 {
		return nil
	}
	var source User
	if err := tx.Select("id", "inviter_id").First(&source, topUp.UserId).Error; err != nil {
		return err
	}
	ancestorID := source.InviterId
	directID := source.Id
	seen := map[int]bool{source.Id: true}
	for level, rate := range []float64{settings.Level1Percent, settings.Level2Percent} {
		if ancestorID <= 0 || seen[ancestorID] {
			break
		}
		var ancestor User
		if err := tx.Select("id", "inviter_id").First(&ancestor, ancestorID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				break
			}
			return err
		}
		seen[ancestor.Id] = true
		quota, err := common.WalletQuotaFromDecimalStrict(decimal.NewFromInt(int64(*topUp.ReferralBaseQuota)).
			Mul(decimal.NewFromFloat(rate)).Div(decimal.NewFromInt(100)).Truncate(0))
		if err != nil {
			return err
		}
		if quota > 0 {
			reward := ReferralReward{
				TopUpID: topUp.Id, UserID: ancestor.Id, InviteeID: source.Id,
				DirectInviteeID: directID, Level: level + 1,
				BaseQuota: *topUp.ReferralBaseQuota, Rate: rate, Quota: quota,
				Status: ReferralRewardPending, CreatedAt: topUp.CompleteTime,
				AvailableAt: topUp.CompleteTime + int64(settings.DelayDays)*86400,
			}
			if err := tx.Create(&reward).Error; err != nil {
				return err
			}
			if settings.DelayDays == 0 {
				if _, err := settleReferralReward(tx, &reward, topUp.CompleteTime); err != nil {
					return err
				}
			}
		}
		directID = ancestor.Id
		ancestorID = ancestor.InviterId
	}
	return nil
}

func settleReferralReward(tx *gorm.DB, reward *ReferralReward, now int64) (bool, error) {
	if reward.Status != ReferralRewardPending || reward.AvailableAt > now {
		return false, nil
	}
	if reward.Quota <= 0 || reward.Quota > common.MaxWalletQuota {
		return false, ErrInvalidTopUpQuota
	}
	result := tx.Model(&User{}).Where("id = ? AND aff_quota <= ? AND aff_history <= ?",
		reward.UserID, common.MaxWalletQuota-reward.Quota, common.MaxWalletQuota-reward.Quota).
		Updates(map[string]any{
			"aff_quota":   gorm.Expr("aff_quota + ?", reward.Quota),
			"aff_history": gorm.Expr("aff_history + ?", reward.Quota),
		})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := tx.Model(&User{}).Where("id = ?", reward.UserID).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			common.SysError(fmt.Sprintf("referral reward %d pending: recipient %d balance limit reached", reward.ID, reward.UserID))
			return false, nil
		}
		return false, tx.Model(reward).Update("status", ReferralRewardCancelled).Error
	}
	result = tx.Model(reward).Where("status = ?", ReferralRewardPending).
		Updates(map[string]any{"status": ReferralRewardSettled, "settled_at": now})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected != 1 {
		return false, errors.New("referral reward settlement conflict")
	}
	if _, err := QueueUserEmail(tx, fmt.Sprintf("referral:%d", reward.ID), reward.UserID,
		"邀请返利已到账", fmt.Sprintf("您获得的第 %d 级邀请返利 %s 已到账邀请奖励余额，可在钱包中查看和划转。", reward.Level, logger.LogQuota(reward.Quota))); err != nil {
		return false, err
	}
	return true, nil
}

// SettleDueReferralRewards resumes the persistent queue after restarts. Keyset
// pagination lets other recipients progress even when one balance is full.
func SettleDueReferralRewards(ctx context.Context, now int64) (int, error) {
	if !operation_setting.IsPaymentComplianceConfirmed() {
		return 0, nil
	}
	settled, lastID := 0, 0
	var settlementErr error
	for {
		var rewards []ReferralReward
		if err := DB.WithContext(ctx).Where("status = ? AND available_at <= ? AND id > ?", ReferralRewardPending, now, lastID).
			Order("id").Limit(200).Find(&rewards).Error; err != nil {
			return settled, err
		}
		if len(rewards) == 0 {
			return settled, settlementErr
		}
		for _, candidate := range rewards {
			lastID = candidate.ID
			credited := false
			err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				var reward ReferralReward
				if err := lockForUpdate(tx).First(&reward, candidate.ID).Error; err != nil {
					return err
				}
				var topUp TopUp
				if err := tx.Select("id", "status").First(&topUp, reward.TopUpID).Error; err != nil {
					return err
				}
				if topUp.Status != common.TopUpStatusSuccess && reward.Status == ReferralRewardPending {
					return tx.Model(&reward).Update("status", ReferralRewardCancelled).Error
				}
				var err error
				credited, err = settleReferralReward(tx, &reward, now)
				return err
			})
			if err != nil {
				settlementErr = err
				common.SysError(fmt.Sprintf("referral reward %d settlement failed: %v", candidate.ID, err))
				if ctx.Err() != nil {
					return settled, ctx.Err()
				}
			} else if credited {
				settled++
			}
		}
	}
}

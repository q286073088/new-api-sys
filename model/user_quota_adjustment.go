package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var (
	ErrInvalidUserQuotaAdjustment = errors.New("invalid user quota adjustment")
	ErrUserQuotaPermission        = errors.New("cannot adjust quota for this user role")
)

// UserQuotaAdjustment is the immutable database snapshot of a committed manual
// adjustment. Pending relay deductions in the quota cache are not part of it.
type UserQuotaAdjustment struct {
	UserID   int
	Username string
	Before   int
	After    int
}

type QuotaCreditOptions struct {
	Gift            bool
	PaidAmountCents int64
}

func AdjustUserQuota(userID, operatorRole int, mode string, value int, options ...QuotaCreditOptions) (*UserQuotaAdjustment, error) {
	credit := QuotaCreditOptions{}
	if len(options) > 0 {
		credit = options[0]
	}
	isGift := credit.Gift
	if credit.PaidAmountCents < 0 || credit.PaidAmountCents > 1000000000000 {
		return nil, ErrInvalidUserQuotaAdjustment
	}
	if userID <= 0 || (mode != "add" && mode != "subtract" && mode != "override") {
		return nil, ErrInvalidUserQuotaAdjustment
	}
	if mode != "override" && value <= 0 {
		return nil, ErrInvalidUserQuotaAdjustment
	}
	if value > common.MaxWalletQuota || value < -common.MaxWalletQuota {
		return nil, ErrWalletQuotaLimitExceeded
	}

	var adjustment UserQuotaAdjustment
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).First(&user, userID).Error; err != nil {
			return err
		}
		if operatorRole != common.RoleRootUser && operatorRole <= user.Role {
			return ErrUserQuotaPermission
		}
		if user.Quota > common.MaxWalletQuota || user.Quota < -common.MaxWalletQuota {
			return ErrWalletQuotaLimitExceeded
		}
		quota := decimal.NewFromInt(int64(value))
		switch mode {
		case "add":
			quota = decimal.NewFromInt(int64(user.Quota)).Add(quota)
		case "subtract":
			quota = decimal.NewFromInt(int64(user.Quota)).Sub(quota)
		}
		after, err := common.WalletQuotaFromDecimalStrict(quota)
		if err != nil {
			return ErrWalletQuotaLimitExceeded
		}
		if mode == "add" {
			orderKey, err := common.GenerateRandomCharsKey(32)
			if err != nil {
				return err
			}
			now := common.GetTimestamp()
			// Administrator credits carry exact quota units, including fractional
			// account amounts. No external cash payment is inferred.
			topUp := TopUp{
				UserId: user.Id, Amount: int64(value), TradeNo: "ADMIN_" + orderKey,
				PaymentMethod: PaymentMethodAdmin, PaymentProvider: PaymentProviderAdmin,
				CreateTime: now, CompleteTime: now, Status: common.TopUpStatusSuccess,
				ReferralBaseQuota: &value,
				IsGift:            isGift,
			}
			if !isGift {
				topUp.InvoiceAmountCents = &credit.PaidAmountCents
				topUp.Money = float64(credit.PaidAmountCents) / 100
			}
			if isGift {
				zero := 0
				topUp.ReferralBaseQuota = &zero
			}
			if err := tx.Create(&topUp).Error; err != nil {
				return err
			}
			if isGift {
				if err := creditTopUpQuota(tx, user.Id, value, nil); err != nil {
					return err
				}
			} else if err := creditTopUpWithReferral(tx, &topUp, value, nil); err != nil {
				return err
			}
		} else if after != user.Quota {
			// An unchanged override succeeds even when MySQL counts changed rows.
			result := tx.Model(&User{}).Where("id = ?", userID).Update("quota", after)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return gorm.ErrRecordNotFound
			}
		}
		adjustment = UserQuotaAdjustment{UserID: user.Id, Username: user.Username, Before: user.Quota, After: after}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Apply only the committed difference, preserving outstanding reservations.
	// Both balances are bounded above, so their difference fits in int64.
	delta := int64(adjustment.After) - int64(adjustment.Before)
	if delta != 0 {
		if err := cacheIncrUserQuota(userID, delta); err != nil {
			common.SysError(fmt.Sprintf("failed to sync manual quota adjustment for user %d: %s", userID, err))
		}
	}
	return &adjustment, nil
}

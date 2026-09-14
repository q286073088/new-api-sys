package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const (
	EpayOrderTopUp        = "topup"
	EpayOrderSubscription = "subscription"
)

// EpayOrderBinding keeps the original verification credential when an
// administrator changes or removes a domain mapping. It is never an API DTO.
type EpayOrderBinding struct {
	Id          int    `json:"-"`
	TradeNo     string `json:"-" gorm:"type:varchar(255);uniqueIndex"`
	OrderType   string `json:"-" gorm:"type:varchar(32)"`
	Domain      string `json:"-" gorm:"type:varchar(253)"`
	MerchantID  string `json:"-" gorm:"type:varchar(64)"`
	MerchantKey string `json:"-" gorm:"type:text"`
	PayAddress  string `json:"-" gorm:"type:text"`
}

func (binding *EpayOrderBinding) insert(tx *gorm.DB, tradeNo, orderType string) error {
	if binding.MerchantID == "" || binding.MerchantKey == "" || tradeNo == "" {
		return errors.New("invalid epay order binding")
	}
	if err := operation_setting.ValidateEpayAddress(binding.PayAddress); err != nil {
		return err
	}
	binding.TradeNo, binding.OrderType = tradeNo, orderType
	// Do not put a payment credential in SQL debug/error logs.
	return tx.Session(&gorm.Session{Logger: gormlogger.Default.LogMode(gormlogger.Silent)}).Create(binding).Error
}

func CreateEpayTopUp(topUp *TopUp, binding *EpayOrderBinding) error {
	if topUp.PaymentProvider != PaymentProviderEpay || binding == nil {
		return ErrPaymentMethodMismatch
	}
	if err := topUp.snapshotReferralBase(); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(topUp).Error; err != nil {
			return err
		}
		return binding.insert(tx, topUp.TradeNo, EpayOrderTopUp)
	})
}

func CreateEpaySubscriptionOrder(order *SubscriptionOrder, binding *EpayOrderBinding) error {
	if order.PaymentProvider != PaymentProviderEpay || binding == nil {
		return ErrPaymentMethodMismatch
	}
	if order.CreateTime == 0 {
		order.CreateTime = common.GetTimestamp()
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(order).Error; err != nil {
			return err
		}
		return binding.insert(tx, order.TradeNo, EpayOrderSubscription)
	})
}

func GetEpayOrderBinding(tradeNo string) (*EpayOrderBinding, error) {
	var binding EpayOrderBinding
	if err := DB.Where("trade_no = ?", tradeNo).First(&binding).Error; err != nil {
		return nil, err
	}
	return &binding, nil
}

// Merge masked keys against the persisted configuration under a row lock, so
// another node's saved credential is not replaced by this node's stale cache.
func updateEpayDomainConfigOption(raw string) error {
	var value string
	err := DB.Transaction(func(tx *gorm.DB) error {
		option := Option{Key: operation_setting.EpayDomainConfigsKey}
		err := lockForUpdate(tx).Where(&Option{Key: option.Key}).First(&option).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var previous []operation_setting.EpayDomainConfig
		if option.Value != "" {
			if err := common.UnmarshalJsonStr(option.Value, &previous); err != nil {
				return errors.New("无法读取已有易支付域名配置")
			}
		}
		items, err := operation_setting.ParseEpayDomainConfigs(raw, previous)
		if err != nil {
			return err
		}
		serialized, err := common.Marshal(items)
		if err != nil {
			return err
		}
		value, option.Value = string(serialized), string(serialized)
		return tx.Session(&gorm.Session{Logger: gormlogger.Default.LogMode(gormlogger.Silent)}).Save(&option).Error
	})
	if err != nil {
		return err
	}
	return updateOptionMap(operation_setting.EpayDomainConfigsKey, value)
}

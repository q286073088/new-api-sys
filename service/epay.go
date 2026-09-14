package service

import (
	"errors"
	"math"
	"regexp"
	"strconv"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var epayCallbackMoneyPattern = regexp.MustCompile(`^[0-9]{1,24}(\.[0-9]{1,8})?$`)

func NewEpayCheckout(host string) (*epay.Client, *model.EpayOrderBinding, error) {
	merchant := operation_setting.GetEpayMerchant(host)
	if merchant.MerchantID == "" || merchant.Key == "" {
		return nil, nil, errors.New("当前域名未配置易支付商户")
	}
	if err := operation_setting.ValidateEpayAddress(merchant.PayAddress); err != nil {
		return nil, nil, err
	}
	client, err := epay.NewClient(&epay.Config{PartnerID: merchant.MerchantID, Key: merchant.Key}, merchant.PayAddress)
	if err != nil {
		return nil, nil, err
	}
	domain, _ := operation_setting.NormalizeEpayDomain(host)
	return client, &model.EpayOrderBinding{
		Domain: domain, MerchantID: merchant.MerchantID, MerchantKey: merchant.Key, PayAddress: merchant.PayAddress,
	}, nil
}

// VerifyEpayPayment selects credentials from the order, never the callback
// hostname or an unverified pid. Pre-upgrade orders use only the legacy default.
func VerifyEpayPayment(params map[string]string, orderType string) (*epay.VerifyRes, error) {
	tradeNo := params["out_trade_no"]
	if tradeNo == "" || len(tradeNo) > 255 {
		return nil, errors.New("invalid epay order number")
	}
	var money float64
	switch orderType {
	case model.EpayOrderTopUp:
		order := model.GetTopUpByTradeNo(tradeNo)
		if order == nil || order.PaymentProvider != model.PaymentProviderEpay {
			return nil, model.ErrTopUpNotFound
		}
		money = order.Money
	case model.EpayOrderSubscription:
		order := model.GetSubscriptionOrderByTradeNo(tradeNo)
		if order == nil || order.PaymentProvider != model.PaymentProviderEpay {
			return nil, model.ErrSubscriptionOrderNotFound
		}
		money = order.Money
	default:
		return nil, errors.New("invalid epay order type")
	}
	merchant := operation_setting.EpayMerchant{}
	binding, err := model.GetEpayOrderBinding(tradeNo)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		merchant = operation_setting.GetEpayMerchant("")
	} else if err != nil {
		return nil, err
	} else {
		if binding.OrderType != orderType {
			return nil, errors.New("epay order type mismatch")
		}
		merchant = operation_setting.EpayMerchant{MerchantID: binding.MerchantID, Key: binding.MerchantKey, PayAddress: binding.PayAddress}
	}
	if merchant.MerchantID == "" || merchant.Key == "" || params["pid"] != merchant.MerchantID {
		return nil, errors.New("epay merchant mismatch")
	}
	if err := operation_setting.ValidateEpayAddress(merchant.PayAddress); err != nil {
		return nil, err
	}
	client, err := epay.NewClient(&epay.Config{PartnerID: merchant.MerchantID, Key: merchant.Key}, merchant.PayAddress)
	if err != nil {
		return nil, err
	}
	verified, err := client.Verify(params)
	if err != nil || verified == nil || !verified.VerifyStatus {
		return nil, errors.New("epay signature verification failed")
	}
	if !epayCallbackMoneyPattern.MatchString(verified.Money) || money <= 0 || math.IsNaN(money) || math.IsInf(money, 0) {
		return nil, errors.New("invalid epay payment amount")
	}
	paid, err := decimal.NewFromString(verified.Money)
	if err != nil || !paid.IsPositive() {
		return nil, errors.New("invalid epay payment amount")
	}
	// Match the exact rounding used by both checkout handlers.
	expected, err := decimal.NewFromString(strconv.FormatFloat(money, 'f', 2, 64))
	if err != nil || !paid.Equal(expected) {
		return nil, errors.New("epay payment amount mismatch")
	}
	return verified, nil
}

func GetCallbackAddress() string {
	if operation_setting.CustomCallbackAddress == "" {
		return system_setting.ServerAddress
	}
	return operation_setting.CustomCallbackAddress
}

package setting

import (
	"errors"
	"math"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
)

const ReferralSettingKey = "ReferralSetting"

type ReferralSetting struct {
	Enabled       bool    `json:"enabled"`
	Level1Percent float64 `json:"level1_percent"`
	Level2Percent float64 `json:"level2_percent"`
	DelayDays     int     `json:"delay_days"`
}

var referralSettings = struct {
	sync.RWMutex
	value ReferralSetting
}{value: ReferralSetting{Level1Percent: 5, DelayDays: 3}}

func GetReferralSetting() ReferralSetting {
	referralSettings.RLock()
	defer referralSettings.RUnlock()
	return referralSettings.value
}

func ParseReferralSetting(value string) (ReferralSetting, error) {
	var settings ReferralSetting
	var parsed *ReferralSetting
	if err := common.UnmarshalJsonStr(value, &parsed); err != nil || parsed == nil {
		return settings, errors.New("无效的邀请返利设置")
	}
	settings = *parsed
	for _, rate := range []float64{settings.Level1Percent, settings.Level2Percent} {
		if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 || rate > 100 ||
			!decimal.NewFromFloat(rate).Equal(decimal.NewFromFloat(rate).Truncate(2)) {
			return settings, errors.New("返利比例必须在 0 到 100 之间，最多保留两位小数")
		}
	}
	if decimal.NewFromFloat(settings.Level1Percent).Add(decimal.NewFromFloat(settings.Level2Percent)).GreaterThan(decimal.NewFromInt(100)) {
		return settings, errors.New("两级返利比例之和不能超过 100%")
	}
	if settings.DelayDays < 0 || settings.DelayDays > 365 {
		return settings, errors.New("返利到账延迟必须是 0 到 365 天的整数")
	}
	return settings, nil
}

func UpdateReferralSetting(value string) error {
	settings, err := ParseReferralSetting(value)
	if err != nil {
		return err
	}
	referralSettings.Lock()
	defer referralSettings.Unlock()
	referralSettings.value = settings
	return nil
}

package operation_setting

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/setting/config"
)

type MonitorSetting struct {
	AutoTestChannelEnabled bool    `json:"auto_test_channel_enabled"`
	AutoTestChannelMinutes float64 `json:"auto_test_channel_minutes"`
	ChannelTestMode        string  `json:"channel_test_mode"`
	ChannelTestConcurrency int     `json:"channel_test_concurrency"`
	ChannelTestPrompt      string  `json:"channel_test_prompt"`
	ChannelTestMaxTokens   int     `json:"channel_test_max_tokens"`
}

const (
	ChannelTestModeScheduledAll    = "scheduled_all"
	ChannelTestModeAutoBanOnly     = "auto_ban_only"
	ChannelTestModePassiveRecovery = "passive_recovery"

	ChannelTestConcurrencyOptionKey = "monitor_setting.channel_test_concurrency"
	DefaultChannelTestConcurrency   = 1
	MaxChannelTestConcurrency       = 32
	ChannelTestPromptOptionKey      = "monitor_setting.channel_test_prompt"
	ChannelTestMaxTokensOptionKey   = "monitor_setting.channel_test_max_tokens"
	DefaultChannelTestPrompt        = "9.11 和 9.9 哪个数更大？请简要说明理由。"
	MaxChannelTestPromptLength      = 20000
	DefaultChannelTestMaxTokens     = 4096
	MaxChannelTestMaxTokens         = 32768
)

// 默认配置
var monitorSetting = MonitorSetting{
	AutoTestChannelEnabled: false,
	AutoTestChannelMinutes: 10,
	ChannelTestMode:        ChannelTestModeScheduledAll,
	ChannelTestConcurrency: DefaultChannelTestConcurrency,
	ChannelTestMaxTokens:   DefaultChannelTestMaxTokens,
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("monitor_setting", &monitorSetting)
}

func GetMonitorSetting() *MonitorSetting {
	if os.Getenv("CHANNEL_TEST_FREQUENCY") != "" {
		frequency, err := strconv.Atoi(os.Getenv("CHANNEL_TEST_FREQUENCY"))
		if err == nil && frequency > 0 {
			monitorSetting.AutoTestChannelEnabled = true
			monitorSetting.AutoTestChannelMinutes = float64(frequency)
			monitorSetting.ChannelTestMode = ChannelTestModeScheduledAll
		}
	}
	if enabled, ok := os.LookupEnv("CHANNEL_TEST_ENABLED"); ok {
		parsed, err := strconv.ParseBool(enabled)
		if err == nil {
			monitorSetting.AutoTestChannelEnabled = parsed
		}
	}
	switch monitorSetting.ChannelTestMode {
	case ChannelTestModeAutoBanOnly, ChannelTestModePassiveRecovery:
	default:
		monitorSetting.ChannelTestMode = ChannelTestModeScheduledAll
	}
	monitorSetting.ChannelTestConcurrency = NormalizeChannelTestConcurrency(monitorSetting.ChannelTestConcurrency)
	return &monitorSetting
}

func (s *MonitorSetting) TestPrompt() string {
	if prompt := strings.TrimSpace(s.ChannelTestPrompt); prompt != "" {
		return prompt
	}
	return DefaultChannelTestPrompt
}

func (s *MonitorSetting) TestMaxTokens() uint {
	if s.ChannelTestMaxTokens < 1 || s.ChannelTestMaxTokens > MaxChannelTestMaxTokens {
		return DefaultChannelTestMaxTokens
	}
	return uint(s.ChannelTestMaxTokens)
}

func ValidateChannelTestPrompt(value string) error {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > MaxChannelTestPromptLength {
		return fmt.Errorf("channel test prompt must be valid text with at most %d characters", MaxChannelTestPromptLength)
	}
	return nil
}

func ValidateChannelTestMaxTokens(value string) error {
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > MaxChannelTestMaxTokens {
		return fmt.Errorf("channel test output limit must be between 1 and %d tokens", MaxChannelTestMaxTokens)
	}
	return nil
}

func NormalizeChannelTestConcurrency(concurrency int) int {
	if concurrency < 1 {
		return DefaultChannelTestConcurrency
	}
	if concurrency > MaxChannelTestConcurrency {
		return MaxChannelTestConcurrency
	}
	return concurrency
}

func ValidateChannelTestConcurrency(value string) error {
	concurrency, err := strconv.Atoi(value)
	if err != nil || concurrency < 1 || concurrency > MaxChannelTestConcurrency {
		return fmt.Errorf("channel test concurrency must be between 1 and %d", MaxChannelTestConcurrency)
	}
	return nil
}

package perf_metrics_setting

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

type PerfMetricsSetting struct {
	Enabled              bool   `json:"enabled"`
	FlushInterval        int    `json:"flush_interval"`
	BucketTime           string `json:"bucket_time"`
	RetentionDays        int    `json:"retention_days"`
	ExcludeErrorsEnabled bool   `json:"exclude_errors_enabled"`
	ExcludedStatusCodes  string `json:"excluded_status_codes"`
}

var perfMetricsSetting = PerfMetricsSetting{
	Enabled:       true,
	FlushInterval: 5,
	BucketTime:    "hour",
	RetentionDays: 0,
}

func init() {
	config.GlobalConfig.Register("perf_metrics_setting", &perfMetricsSetting)
}

func GetSetting() PerfMetricsSetting {
	return perfMetricsSetting
}

func ParseExcludedStatusCodes(value string) ([]int, error) {
	codes := []int{}
	for _, token := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' || r == ' ' }) {
		code, err := strconv.Atoi(token)
		if err != nil || code < 100 || code > 599 {
			return nil, fmt.Errorf("invalid HTTP status code: %s", token)
		}
		codes = append(codes, code)
	}
	return codes, nil
}

func (s PerfMetricsSetting) ExcludesStatus(code int) bool {
	if !s.ExcludeErrorsEnabled || code == 0 {
		return false
	}
	codes, _ := ParseExcludedStatusCodes(s.ExcludedStatusCodes)
	return slices.Contains(codes, code)
}

func GetBucketSeconds() int64 {
	switch perfMetricsSetting.BucketTime {
	case "minute":
		return 60
	case "5min":
		return 300
	case "hour":
		return 3600
	default:
		return 3600
	}
}

func GetFlushIntervalMinutes() int {
	if perfMetricsSetting.FlushInterval < 1 {
		return 1
	}
	return perfMetricsSetting.FlushInterval
}

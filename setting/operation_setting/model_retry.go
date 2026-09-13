package operation_setting

import (
	"errors"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

var modelRetryMu sync.RWMutex
var modelRetryTimes = map[string]int{}

func ParseModelRetryTimes(value string) (map[string]int, error) {
	var parsed map[string]*int
	if err := common.UnmarshalJsonStr(value, &parsed); err != nil || parsed == nil {
		return nil, errors.New("model retry counts must be a JSON object")
	}
	counts := make(map[string]int, len(parsed))
	for name, count := range parsed {
		if strings.TrimSpace(name) == "" || name != strings.TrimSpace(name) || count == nil || *count < 0 || *count > 10 {
			return nil, errors.New("model retry counts require exact model names and integers from 0 to 10")
		}
		counts[name] = *count
	}
	return counts, nil
}

func UpdateModelRetryTimes(value string) error {
	parsed, err := ParseModelRetryTimes(value)
	if err != nil {
		return err
	}
	modelRetryMu.Lock()
	modelRetryTimes = parsed
	modelRetryMu.Unlock()
	return nil
}

func ModelRetryTimesJSON() string {
	modelRetryMu.RLock()
	defer modelRetryMu.RUnlock()
	return common.GetJsonString(modelRetryTimes)
}

// GetModelRetryTimes returns the retry count and whether the model overrides it.
func GetModelRetryTimes(modelName string) (int, bool) {
	modelRetryMu.RLock()
	count, exists := modelRetryTimes[modelName]
	modelRetryMu.RUnlock()
	if exists {
		return count, true
	}
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.RetryTimes, false
}

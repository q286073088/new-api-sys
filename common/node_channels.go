package common

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
)

// This is process-local configuration, never a shared channel status change.
var nodeExcludedChannels atomic.Pointer[map[int]struct{}]

func ConfigureNodeExcludedChannels(value string) error {
	ids := make(map[int]struct{})
	if strings.TrimSpace(value) != "" {
		for part := range strings.SplitSeq(value, ",") {
			id, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil || id <= 0 {
				return fmt.Errorf("NODE_EXCLUDED_CHANNEL_IDS must contain comma-separated positive channel IDs; invalid entry %q", part)
			}
			ids[id] = struct{}{}
		}
	}
	nodeExcludedChannels.Store(&ids)
	return nil
}

func IsChannelExcludedOnNode(channelID int) bool {
	ids := nodeExcludedChannels.Load()
	if ids == nil {
		return false
	}
	_, excluded := (*ids)[channelID]
	return excluded
}

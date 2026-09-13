package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelHealthProbeDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			setupReferralDatabase(t, dialect, false)
			oldMemory := common.MemoryCacheEnabled
			common.MemoryCacheEnabled = false
			t.Cleanup(func() { common.MemoryCacheEnabled = oldMemory })
			channel := Channel{Name: "probe", Key: "manual\nauto-a\nauto-b", Status: common.ChannelStatusAutoDisabled,
				ChannelInfo: ChannelInfo{IsMultiKey: true, MultiKeySize: 3, MultiKeyMode: constant.MultiKeyModePolling,
					MultiKeyStatusList: map[int]int{0: common.ChannelStatusManuallyDisabled, 1: common.ChannelStatusAutoDisabled, 2: common.ChannelStatusAutoDisabled}}}
			require.NoError(t, DB.Create(&channel).Error)
			require.NoError(t, DB.Create(&Ability{ChannelId: channel.Id, Group: "default", Model: "test", Enabled: false}).Error)
			_, _, relayErr := channel.GetNextEnabledKey()
			require.NotNil(t, relayErr, "real requests must still reject disabled keys")
			for _, wantKey := range []string{"auto-a", "auto-b"} {
				key, _, testErr := channel.GetNextTestKey()
				require.Nil(t, testErr)
				assert.Equal(t, wantKey, key)
				require.NoError(t, DB.First(&channel, channel.Id).Error)
				assert.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
				assert.Equal(t, common.ChannelStatusAutoDisabled, channel.ChannelInfo.MultiKeyStatusList[1])
			}
			require.True(t, UpdateChannelStatus(channel.Id, "auto-b", common.ChannelStatusEnabled, ""))
			require.NoError(t, DB.First(&channel, channel.Id).Error)
			assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
			assert.Equal(t, common.ChannelStatusManuallyDisabled, channel.ChannelInfo.MultiKeyStatusList[0])
			key, _, relayErr := channel.GetNextEnabledKey()
			require.Nil(t, relayErr)
			assert.Equal(t, "auto-b", key)
			key, _, testErr := channel.GetNextTestKey()
			require.Nil(t, testErr)
			assert.Equal(t, "auto-b", key, "enabled channels must continue testing a usable key")

			require.True(t, UpdateChannelStatus(channel.Id, "auto-b", common.ChannelStatusAutoDisabled, "offline"))
			require.NoError(t, DB.First(&channel, channel.Id).Error)
			channelSyncLock.Lock()
			previousChannels := channelsIDM
			previousGroups, previousConfigs := group2model2channels, channel2advancedCustomConfig
			channelSyncLock.Unlock()
			t.Cleanup(func() {
				channelSyncLock.Lock()
				channelsIDM = previousChannels
				group2model2channels, channel2advancedCustomConfig = previousGroups, previousConfigs
				channelSyncLock.Unlock()
			})
			common.MemoryCacheEnabled = true
			for _, mode := range []constant.MultiKeyMode{constant.MultiKeyModePolling, constant.MultiKeyModeRandom} {
				channel.ChannelInfo.MultiKeyMode, channel.ChannelInfo.MultiKeyPollingIndex = mode, 0
				require.NoError(t, channel.SaveChannelInfo())
				cached := channel
				channelSyncLock.Lock()
				channelsIDM = map[int]*Channel{channel.Id: &cached}
				channelSyncLock.Unlock()
				for _, wantKey := range []string{"auto-a", "auto-b"} {
					// Scheduled checks load fresh database snapshots, not cache pointers.
					require.NoError(t, DB.First(&channel, channel.Id).Error)
					key, _, testErr := channel.GetNextTestKey()
					require.Nil(t, testErr)
					assert.Equal(t, wantKey, key, "cached health checks must rotate disabled keys in %s mode", mode)
					InitChannelCache()
				}
			}
		})
	}
}

func setupChannelStatusTest(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)

	memoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = memoryCacheEnabled
	})
}

func TestUpdateChannelStatusPersistsMultiKeyState(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "multi-key-status",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 1,
		},
	}
	require.NoError(t, DB.Create(&channel).Error)

	changed := UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusAutoDisabled, "provider rejected key")
	require.True(t, changed)

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
	assert.Equal(t, "provider rejected key", stored.ChannelInfo.MultiKeyDisabledReason[0])
	assert.NotZero(t, stored.ChannelInfo.MultiKeyDisabledTime[0])
	assert.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)
}

func TestSaveStatusStateFromSingleKeySnapshotPreservesUnownedColumns(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:        "single-key-status",
		Key:         "original-key",
		Status:      common.ChannelStatusEnabled,
		Models:      "original-model",
		Group:       "default",
		UsedQuota:   100,
		ChannelInfo: ChannelInfo{},
	}
	require.NoError(t, DB.Create(&channel).Error)

	stale, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)

	concurrentChannelInfo := ChannelInfo{
		IsMultiKey:           true,
		MultiKeySize:         2,
		MultiKeyMode:         constant.MultiKeyModePolling,
		MultiKeyPollingIndex: 1,
	}
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Updates(map[string]any{
		"key":          "rotated-key",
		"used_quota":   gorm.Expr("used_quota + ?", 250),
		"models":       "concurrent-model",
		"channel_info": concurrentChannelInfo,
	}).Error)

	stale.Status = common.ChannelStatusManuallyDisabled
	stale.SetOtherInfo(map[string]any{
		"status_reason": "manual operation",
		"status_time":   int64(1234),
	})
	require.NoError(t, stale.saveStatusState())

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)
	assert.Equal(t, "rotated-key", stored.Key)
	assert.Equal(t, int64(350), stored.UsedQuota)
	assert.Equal(t, "concurrent-model", stored.Models)
	assert.Equal(t, concurrentChannelInfo, stored.ChannelInfo)

	otherInfo := stored.GetOtherInfo()
	assert.Equal(t, "manual operation", otherInfo["status_reason"])
	assert.Equal(t, float64(1234), otherInfo["status_time"])
}

package model

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm/clause"
)

// Announcement views belong to an account, so another browser or device does
// not display an announcement that the user has already seen.
type AnnouncementView struct {
	ID        int
	UserID    int    `gorm:"uniqueIndex:idx_announcement_user_key"`
	Key       string `gorm:"type:varchar(128);uniqueIndex:idx_announcement_user_key"`
	CreatedAt int64
}

func GetAnnouncementViews(ctx context.Context, userID int) ([]string, error) {
	keys := make([]string, 0)
	err := DB.WithContext(ctx).Model(&AnnouncementView{}).Where("user_id = ?", userID).Pluck("key", &keys).Error
	return keys, err
}

func MarkAnnouncementViewed(ctx context.Context, userID int, key string) error {
	return DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&AnnouncementView{
		UserID: userID, Key: key, CreatedAt: common.GetTimestamp(),
	}).Error
}

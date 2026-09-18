package model

import (
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SetUserInviter serializes relationship edits across nodes before walking the
// ancestry, so concurrent A->B and B->A edits cannot both pass validation.
func SetUserInviter(tx *gorm.DB, userID, inviterID int) error {
	if inviterID < 0 || inviterID == userID {
		return errors.New("invalid inviter: a user cannot invite themselves")
	}
	guard := Option{Key: "referral_relationship_lock", Value: "1"}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&guard).Error; err != nil {
		return err
	}
	if err := lockForUpdate(tx).Where(&Option{Key: guard.Key}).First(&guard).Error; err != nil {
		return err
	}
	var user User
	if err := tx.First(&user, userID).Error; err != nil {
		return err
	}
	if user.InviterId == inviterID {
		return nil
	}
	seen := map[int]bool{userID: true}
	for ancestorID := inviterID; ancestorID != 0; {
		if seen[ancestorID] {
			return errors.New("inviter relationship would create a cycle")
		}
		seen[ancestorID] = true
		var ancestor User
		if err := tx.Select("id", "inviter_id").First(&ancestor, ancestorID).Error; err != nil {
			return err
		}
		ancestorID = ancestor.InviterId
	}
	previousID := user.InviterId
	if err := tx.Model(&user).Update("inviter_id", inviterID).Error; err != nil {
		return err
	}
	for _, id := range []int{previousID, inviterID} {
		if id == 0 {
			continue
		}
		var count int64
		if err := tx.Model(&User{}).Where("inviter_id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if err := tx.Model(&User{}).Where("id = ?", id).Update("aff_count", count).Error; err != nil {
			return err
		}
	}
	return nil
}

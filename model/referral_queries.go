package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
)

var ErrReferralNotOwned = errors.New("无权查看该用户的邀请团队")

type ReferralSummary struct {
	DirectCount    int64 `json:"direct_count"`
	TeamCount      int64 `json:"team_count"`
	AvailableQuota int   `json:"available_quota"`
	TotalEarned    int   `json:"total_earned"`
	PendingQuota   int64 `json:"pending_quota"`
}

type ReferralInvitee struct {
	ID                int    `json:"id"`
	Username          string `json:"username"`
	DisplayName       string `json:"display_name"`
	CreatedAt         int64  `json:"created_at"`
	TopUpQuota        int    `json:"topup_quota"`
	DirectRewardQuota int64  `json:"direct_reward_quota"`
	TeamRewardQuota   int64  `json:"team_reward_quota"`
	PendingQuota      int64  `json:"pending_quota"`
}

type ReferralRewardDetail struct {
	ReferralReward
	InviteeUsername string `json:"invitee_username"`
}

func GetReferralSummary(userID int) (*ReferralSummary, error) {
	var summary ReferralSummary
	if err := DB.Model(&User{}).Select("aff_quota AS available_quota, aff_history AS total_earned").
		Where("id = ?", userID).Take(&summary).Error; err != nil {
		return nil, err
	}
	direct := DB.Model(&User{}).Select("id").Where("inviter_id = ? AND id <> ?", userID, userID)
	if err := DB.Model(&User{}).Where("inviter_id = ? AND id <> ?", userID, userID).Count(&summary.DirectCount).Error; err != nil {
		return nil, err
	}
	if err := DB.Model(&User{}).Where("inviter_id IN (?) AND id <> ? AND id NOT IN (?)", direct, userID, direct).
		Count(&summary.TeamCount).Error; err != nil {
		return nil, err
	}
	if err := DB.Model(&ReferralReward{}).Where("user_id = ? AND status = ?", userID, ReferralRewardPending).
		Select("COALESCE(SUM(quota), 0)").Scan(&summary.PendingQuota).Error; err != nil {
		return nil, err
	}
	return &summary, nil
}

func GetReferralInvitees(userID, parentID int, page *common.PageInfo) ([]ReferralInvitee, int64, error) {
	if parentID == 0 {
		parentID = userID
	} else if parentID != userID {
		var count int64
		if err := DB.Model(&User{}).Where("id = ? AND inviter_id = ?", parentID, userID).Count(&count).Error; err != nil {
			return nil, 0, err
		}
		if count != 1 {
			return nil, 0, ErrReferralNotOwned
		}
	}
	query := DB.Model(&User{}).Where("inviter_id = ? AND id <> ? AND id <> ?", parentID, userID, parentID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]ReferralInvitee, 0)
	if err := query.Select("id", "username", "display_name", "created_at").Order("id DESC").
		Limit(page.GetPageSize()).Offset(page.GetStartIdx()).Scan(&items).Error; err != nil {
		return nil, 0, err
	}
	if len(items) == 0 {
		return items, total, nil
	}
	ids := make([]int, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	var topUps []struct {
		UserID int
		Quota  decimal.Decimal
	}
	// Provider order amounts have different units. Manual credits store exact
	// quota units; subscription/balance records are not wallet top-ups.
	if err := DB.Model(&TopUp{}).Where("user_id IN ? AND status = ? AND amount > 0 AND payment_provider <> ? AND payment_method <> ?",
		ids, common.TopUpStatusSuccess, PaymentProviderBalance, PaymentMethodBalance).Where("(is_gift = ? OR is_gift IS NULL)", false).
		Select("user_id, SUM(CASE WHEN payment_provider = ? OR (payment_provider = '' AND payment_method = ?) THEN money * ? WHEN payment_provider IN ? OR (payment_provider = '' AND payment_method IN ?) THEN amount ELSE amount * ? END) AS quota",
			PaymentProviderStripe, PaymentMethodStripe, common.QuotaPerUnit, []string{PaymentProviderCreem, PaymentProviderAdmin}, []string{PaymentMethodCreem, PaymentMethodAdmin}, common.QuotaPerUnit).
		Group("user_id").Scan(&topUps).Error; err != nil {
		return nil, 0, err
	}
	topUpTotals := make(map[int]int, len(topUps))
	for _, topUp := range topUps {
		quota, err := common.WalletQuotaFromDecimalStrict(topUp.Quota)
		if err != nil {
			return nil, 0, err
		}
		topUpTotals[topUp.UserID] = quota
	}
	var rewards []struct {
		InviteeID int
		Direct    int64
		Team      int64
		Pending   int64
	}
	groupColumn := "direct_invitee_id"
	if parentID != userID {
		groupColumn = "invitee_id"
	}
	if err := DB.Model(&ReferralReward{}).Where("user_id = ? AND "+groupColumn+" IN ? AND status IN ?",
		userID, ids, []string{ReferralRewardPending, ReferralRewardSettled}).
		Select(groupColumn+" AS invitee_id, SUM(CASE WHEN level = 1 THEN quota ELSE 0 END) AS direct, SUM(CASE WHEN level = 2 THEN quota ELSE 0 END) AS team, SUM(CASE WHEN status = ? THEN quota ELSE 0 END) AS pending", ReferralRewardPending).
		Group(groupColumn).Scan(&rewards).Error; err != nil {
		return nil, 0, err
	}
	for i := range items {
		items[i].TopUpQuota = topUpTotals[items[i].ID]
		for _, reward := range rewards {
			if reward.InviteeID == items[i].ID {
				items[i].DirectRewardQuota = reward.Direct
				items[i].TeamRewardQuota = reward.Team
				items[i].PendingQuota = reward.Pending
				break
			}
		}
	}
	return items, total, nil
}

func GetReferralRewards(userID int, page *common.PageInfo) ([]ReferralRewardDetail, int64, error) {
	var total int64
	if err := DB.Model(&ReferralReward{}).Where("user_id = ?", userID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]ReferralRewardDetail, 0)
	err := DB.Model(&ReferralReward{}).
		Select("referral_rewards.*, users.username AS invitee_username").
		Joins("LEFT JOIN users ON users.id = referral_rewards.invitee_id").
		Where("referral_rewards.user_id = ?", userID).Order("referral_rewards.id DESC").
		Limit(page.GetPageSize()).Offset(page.GetStartIdx()).Scan(&items).Error
	return items, total, err
}

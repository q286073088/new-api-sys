package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type TopUpDetail struct {
	TopUp
	Username      string `json:"username"`
	DisplayName   string `json:"display_name"`
	CreditedQuota int64  `json:"credited_quota"`
}

type TopUpUserSummary struct {
	OrderCount         int64   `json:"order_count"`
	SuccessCount       int64   `json:"success_count"`
	GiftCount          int64   `json:"gift_count"`
	TotalPaidMoney     float64 `json:"total_paid_money"`
	TotalCreditedQuota int64   `json:"total_credited_quota"`
	GiftQuota          int64   `json:"gift_quota"`
	FirstSuccessAt     int64   `json:"first_success_at"`
	LastSuccessAt      int64   `json:"last_success_at"`
}

type TopUpDetailFilter struct {
	Keyword       string
	Status        string
	PaymentMethod string
	From          int64
	To            int64
}

// CreditedQuota normalizes provider-specific amount fields to exact quota units.
// Epay/Waffo/Stripe checkout amounts are display-currency units, while Creem
// products and administrator credits already carry quota units.
func (topUp *TopUp) CreditedQuota() int64 {
	amount := decimal.NewFromInt(topUp.Amount)
	switch {
	case topUp.PaymentProvider == PaymentProviderStripe,
		topUp.PaymentProvider == "" && topUp.PaymentMethod == PaymentMethodStripe:
		amount = decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	case topUp.PaymentProvider == PaymentProviderCreem,
		topUp.PaymentProvider == PaymentProviderAdmin,
		topUp.PaymentProvider == "" && (topUp.PaymentMethod == PaymentMethodCreem || topUp.PaymentMethod == PaymentMethodAdmin):
	default:
		amount = amount.Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	}
	quota, err := common.WalletQuotaFromDecimalStrict(amount)
	if err != nil {
		return 0
	}
	return int64(quota)
}

func topUpDetailQuery(filter TopUpDetailFilter) (*gorm.DB, error) {
	query := DB.Model(&TopUp{}).
		Select("top_ups.*, users.username AS username, users.display_name AS display_name").
		Joins("LEFT JOIN users ON users.id = top_ups.user_id")
	if filter.Keyword != "" {
		pattern, err := sanitizeLikePattern(filter.Keyword)
		if err != nil {
			return nil, err
		}
		likePattern := "%" + pattern + "%"
		query = query.Where(
			"(top_ups.trade_no LIKE ? ESCAPE '!' OR users.username LIKE ? ESCAPE '!' OR users.display_name LIKE ? ESCAPE '!')",
			likePattern, likePattern, likePattern,
		)
	}
	if filter.Status != "" {
		query = query.Where("top_ups.status = ?", filter.Status)
	}
	if filter.PaymentMethod != "" {
		query = query.Where("top_ups.payment_method = ?", filter.PaymentMethod)
	}
	if filter.From > 0 {
		query = query.Where("top_ups.create_time >= ?", filter.From)
	}
	if filter.To > 0 {
		query = query.Where("top_ups.create_time < ?", filter.To)
	}
	return query, nil
}

func GetTopUpDetails(filter TopUpDetailFilter, page *common.PageInfo) ([]TopUpDetail, int64, error) {
	query, err := topUpDetailQuery(filter)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]TopUpDetail, 0, page.GetPageSize())
	if err := query.Order("top_ups.id DESC").Limit(page.GetPageSize()).Offset(page.GetStartIdx()).Scan(&items).Error; err != nil {
		return nil, 0, err
	}
	for i := range items {
		items[i].CreditedQuota = items[i].TopUp.CreditedQuota()
	}
	return items, total, nil
}

func GetTopUpDetail(id int) (*TopUpDetail, error) {
	var item TopUpDetail
	err := DB.Model(&TopUp{}).
		Select("top_ups.*, users.username AS username, users.display_name AS display_name").
		Joins("LEFT JOIN users ON users.id = top_ups.user_id").
		Where("top_ups.id = ?", id).
		Scan(&item).Error
	if err != nil {
		return nil, err
	}
	if item.Id == 0 {
		return nil, errors.New("充值订单不存在")
	}
	item.CreditedQuota = item.TopUp.CreditedQuota()
	return &item, nil
}

func GetTopUpUserSummary(userID int) (TopUpUserSummary, error) {
	var summary TopUpUserSummary
	var orders []TopUp
	if err := DB.Select("id", "amount", "money", "payment_method", "payment_provider", "is_gift", "complete_time").
		Where("user_id = ? AND status = ?", userID, common.TopUpStatusSuccess).
		Order("complete_time ASC").Find(&orders).Error; err != nil {
		return summary, err
	}
	for _, order := range orders {
		summary.SuccessCount++
		summary.TotalCreditedQuota += order.CreditedQuota()
		if order.IsGift {
			summary.GiftCount++
			summary.GiftQuota += order.CreditedQuota()
			continue
		}
		summary.TotalPaidMoney += order.Money
		if summary.FirstSuccessAt == 0 {
			summary.FirstSuccessAt = order.CompleteTime
		}
		summary.LastSuccessAt = order.CompleteTime
	}
	if err := DB.Model(&TopUp{}).Where("user_id = ?", userID).Count(&summary.OrderCount).Error; err != nil {
		return summary, err
	}
	return summary, nil
}

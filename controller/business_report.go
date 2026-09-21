package controller

import (
	"encoding/csv"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type businessReport struct {
	UpdatedAt      int64            `json:"updated_at"`
	Timezone       string           `json:"timezone"`
	Overview       map[string]any   `json:"overview"`
	Periods        map[string]any   `json:"periods"`
	Trend          []map[string]any `json:"trend"`
	PaymentQuality map[string]any   `json:"payment_quality"`
	Referral       map[string]any   `json:"referral"`
	Gifts          map[string]any   `json:"gifts"`
}

func reportBounds(c *gin.Context) (time.Time, time.Time) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	start := today.AddDate(0, 0, -29)
	end := today.AddDate(0, 0, 1).Add(-time.Nanosecond)
	if raw := c.Query("start"); raw != "" {
		if t, err := time.ParseInLocation("2006-01-02", raw, loc); err == nil {
			start = t
		}
	}
	if raw := c.Query("end"); raw != "" {
		if t, err := time.ParseInLocation("2006-01-02", raw, loc); err == nil {
			end = t.AddDate(0, 0, 1).Add(-time.Nanosecond)
		}
	}
	return start, end
}

func moneyBetween(start, end time.Time) (float64, int64) {
	var row struct {
		Money float64
		Count int64
	}
	model.DB.Model(&model.TopUp{}).Select("COALESCE(SUM(money),0) AS money, COUNT(*) AS count").Where("status = ? AND (is_gift = ? OR is_gift IS NULL) AND complete_time >= ? AND complete_time <= ?", common.TopUpStatusSuccess, false, start.Unix(), end.Unix()).Scan(&row)
	return row.Money, row.Count
}

func userCountBetween(start, end time.Time) int64 {
	var n int64
	model.DB.Model(&model.User{}).Where("created_at >= ? AND created_at <= ?", start.Unix(), end.Unix()).Count(&n)
	return n
}

func referralStats(start, end time.Time) (int64, float64, float64) {
	var registrations int64
	model.DB.Model(&model.User{}).Where("inviter_id > 0 AND created_at >= ? AND created_at <= ?", start.Unix(), end.Unix()).Count(&registrations)
	var rows []struct {
		Level int
		Quota int64
	}
	model.DB.Model(&model.ReferralReward{}).Select("level, COALESCE(SUM(quota),0) AS quota").Where("created_at >= ? AND created_at <= ? AND status <> ?", start.Unix(), end.Unix(), model.ReferralRewardCancelled).Group("level").Find(&rows)
	var direct, team float64
	for _, row := range rows {
		if row.Level == 1 {
			direct += float64(row.Quota) / common.QuotaPerUnit
		} else if row.Level == 2 {
			team += float64(row.Quota) / common.QuotaPerUnit
		}
	}
	return registrations, direct, team
}

func GetBusinessReport(c *gin.Context) {
	start, end := reportBounds(c)
	now := time.Now()
	revenue, _ := moneyBetween(time.Unix(0, 0), end)
	paidUsers := int64(0)
	model.DB.Model(&model.TopUp{}).Where("status = ? AND (is_gift = ? OR is_gift IS NULL) AND money > 0", common.TopUpStatusSuccess, false).Distinct("user_id").Count(&paidUsers)
	totalUsers := int64(0)
	model.DB.Model(&model.User{}).Count(&totalUsers)
	today := time.Now().In(start.Location())
	dayStart := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	dayEnd := dayStart.AddDate(0, 0, 1).Add(-time.Nanosecond)
	dayRevenue, _ := moneyBetween(dayStart, dayEnd)
	weekStart := dayStart.AddDate(0, 0, -(int(dayStart.Weekday())+6)%7)
	weekRevenue, _ := moneyBetween(weekStart, dayEnd)
	monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())
	monthRevenue, _ := moneyBetween(monthStart, dayEnd)
	trend := make([]map[string]any, 0)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		dayEnd := d.AddDate(0, 0, 1).Add(-time.Nanosecond)
		r, _ := moneyBetween(d, dayEnd)
		trend = append(trend, map[string]any{"date": d.Format("2006-01-02"), "revenue": r})
	}
	reg, direct, team := referralStats(start, end)
	gift, _ := moneyBetween(time.Unix(0, 0), end)
	var giftMoney float64
	model.DB.Model(&model.TopUp{}).Select("COALESCE(SUM(money),0)").Where("status = ? AND is_gift = ? AND complete_time <= ?", common.TopUpStatusSuccess, true, end.Unix()).Scan(&giftMoney)
	report := businessReport{UpdatedAt: now.Unix(), Timezone: "UTC+8", Overview: map[string]any{"registered_users": totalUsers, "paid_users": paidUsers, "paid_conversion": func() float64 {
		if totalUsers == 0 {
			return 0
		}
		return float64(paidUsers) * 100 / float64(totalUsers)
	}(), "revenue": revenue}, Periods: map[string]any{"today": dayRevenue, "week": weekRevenue, "month": monthRevenue, "total": revenue}, Trend: trend, PaymentQuality: map[string]any{"arppu": func() float64 {
		if paidUsers == 0 {
			return 0
		}
		return revenue / float64(paidUsers)
	}(), "arpu": func() float64 {
		if totalUsers == 0 {
			return 0
		}
		return revenue / float64(totalUsers)
	}(), "paid_users": paidUsers}, Referral: map[string]any{"registrations": reg, "direct_rebate": direct, "team_rebate": team, "total_rebate": direct + team}, Gifts: map[string]any{"amount": giftMoney, "quota_orders": gift}}
	common.ApiSuccess(c, report)
}

func ExportBusinessReport(c *gin.Context) { GetBusinessReportCSV(c) }
func GetBusinessReportCSV(c *gin.Context) {
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=business-report.csv")
	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{"date", "revenue"})
	start, end := reportBounds(c)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		dayEnd := d.AddDate(0, 0, 1).Add(-time.Nanosecond)
		r, _ := moneyBetween(d, dayEnd)
		_ = w.Write([]string{d.Format("2006-01-02"), strconv.FormatInt(int64(r), 10)})
	}
	w.Flush()
}

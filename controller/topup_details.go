package controller

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func topUpDetailFilter(c *gin.Context) (model.TopUpDetailFilter, error) {
	filter := model.TopUpDetailFilter{
		Keyword:       c.Query("keyword"),
		Status:        c.Query("status"),
		PaymentMethod: c.Query("payment_method"),
	}
	var parseErr error
	filter.From, parseErr = strconv.ParseInt(c.DefaultQuery("from", "0"), 10, 64)
	if parseErr != nil {
		return filter, errors.New("Invalid start time")
	}
	filter.To, parseErr = strconv.ParseInt(c.DefaultQuery("to", "0"), 10, 64)
	if parseErr != nil {
		return filter, errors.New("Invalid end time")
	}
	if filter.Status != "" && !isTopUpStatus(filter.Status) {
		return filter, errors.New("Invalid order status")
	}
	return filter, nil
}

func isTopUpStatus(status string) bool {
	switch status {
	case common.TopUpStatusPending, common.TopUpStatusSuccess, common.TopUpStatusFailed, common.TopUpStatusExpired:
		return true
	}
	return false
}

// GetTopUpDetails shows successful and historical orders for auditors, without
// changing settlement behavior.
func GetTopUpDetails(c *gin.Context) {
	filter, err := topUpDetailFilter(c)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	page := common.GetPageQuery(c)
	items, total, err := model.GetTopUpDetails(filter, page)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(items)
	common.ApiSuccess(c, page)
}

func GetTopUpDetail(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "Invalid order ID")
		return
	}
	item, err := model.GetTopUpDetail(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func GetTopUpDetailSummary(c *gin.Context) {
	userID, err := strconv.Atoi(c.Param("userId"))
	if err != nil || userID <= 0 {
		common.ApiErrorMsg(c, "Invalid user ID")
		return
	}
	summary, err := model.GetTopUpUserSummary(userID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, summary)
}

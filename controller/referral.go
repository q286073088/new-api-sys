package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

func GetReferralSummary(c *gin.Context) {
	summary, err := model.GetReferralSummary(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"summary": summary, "settings": setting.GetReferralSetting()})
}

func GetReferralInvitees(c *gin.Context) {
	parentID, err := strconv.Atoi(c.DefaultQuery("parent_id", "0"))
	if err != nil || parentID < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的邀请用户 ID"})
		return
	}
	page := common.GetPageQuery(c)
	if page.Page < 1 || page.PageSize < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的分页参数"})
		return
	}
	items, total, err := model.GetReferralInvitees(c.GetInt("id"), parentID, page)
	if errors.Is(err, model.ErrReferralNotOwned) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(items)
	common.ApiSuccess(c, page)
}

func GetReferralRewards(c *gin.Context) {
	page := common.GetPageQuery(c)
	if page.Page < 1 || page.PageSize < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的分页参数"})
		return
	}
	items, total, err := model.GetReferralRewards(c.GetInt("id"), page)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(items)
	common.ApiSuccess(c, page)
}

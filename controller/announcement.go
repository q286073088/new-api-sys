package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetAnnouncementViews(c *gin.Context) {
	keys, err := model.GetAnnouncementViews(c.Request.Context(), c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": keys})
}

func MarkAnnouncementViewed(c *gin.Context) {
	var req struct {
		Key string `json:"key"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || strings.TrimSpace(req.Key) == "" || len(req.Key) > 128 {
		common.ApiErrorMsg(c, "Invalid announcement key")
		return
	}
	if err := model.MarkAnnouncementViewed(c.Request.Context(), c.GetInt("id"), req.Key); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

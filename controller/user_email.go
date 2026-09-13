package controller

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func SendUserEmails(c *gin.Context) {
	var req struct {
		IDs       []int  `json:"ids"`
		AllUsers  bool   `json:"all_users"`
		RequestID string `json:"request_id"`
		Subject   string `json:"subject"`
		Content   string `json:"content"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 100*1024)
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "Invalid email request")
		return
	}
	requestID, err := uuid.Parse(req.RequestID)
	req.Subject, req.Content = strings.TrimSpace(req.Subject), strings.TrimSpace(req.Content)
	if err != nil || req.AllUsers == (len(req.IDs) > 0) || len(req.IDs) > 500 ||
		req.Subject == "" || utf8.RuneCountInString(req.Subject) > 160 || strings.ContainsAny(req.Subject, "\r\n") ||
		req.Content == "" || utf8.RuneCountInString(req.Content) > 20000 || slices.ContainsFunc(req.IDs, func(id int) bool { return id <= 0 }) {
		common.ApiErrorMsg(c, "Invalid recipients, subject or content")
		return
	}
	if common.SMTPServer == "" || (common.SMTPFrom == "" && common.SMTPAccount == "") {
		common.ApiErrorMsg(c, "Configure SMTP before sending emails")
		return
	}
	myRole, operatorID := c.GetInt("role"), c.GetInt("id")
	if myRole < common.RoleAdminUser || operatorID <= 0 {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "Permission denied"})
		return
	}
	slices.Sort(req.IDs)
	req.IDs = slices.Compact(req.IDs)
	queued, skipped, matched := 0, 0, 0
	err = model.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&model.User{})
		if req.AllUsers {
			if myRole != common.RoleRootUser {
				query = query.Where("role < ?", myRole)
			}
		} else {
			query = query.Where("id IN ?", req.IDs)
		}
		var users []model.User
		return query.Select("id", "role").FindInBatches(&users, 200, func(_ *gorm.DB, _ int) error {
			for _, user := range users {
				if !canManageTargetRole(myRole, user.Role) {
					return errors.New("Permission denied for one or more recipients")
				}
				matched++
				added, err := model.QueueUserEmail(tx, fmt.Sprintf("admin:%d:%s:%d", operatorID, requestID.String(), user.Id), user.Id, req.Subject, req.Content)
				if err != nil {
					return err
				}
				if added {
					queued++
				} else {
					skipped++
				}
			}
			return nil
		}).Error
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !req.AllUsers {
		skipped += len(req.IDs) - matched
	}
	recordManageAudit(c, "user.email", map[string]any{"all_users": req.AllUsers, "queued": queued, "skipped": skipped})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"queued": queued, "skipped": skipped}})
}

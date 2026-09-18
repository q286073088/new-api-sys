package controller

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

func GetInvoiceSummary(c *gin.Context) {
	summary, err := model.InvoiceSummaryForUser(model.DB, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	invoiceSettings := setting.GetInvoiceSetting()
	common.ApiSuccess(c, gin.H{"summary": summary, "settings": gin.H{"enabled": invoiceSettings.Enabled, "allowed": invoiceSettings.Allows(c.GetInt("id"))}})
}

func GetUserInvoices(c *gin.Context) {
	page := common.GetPageQuery(c)
	var items []model.InvoiceApplication
	query := model.DB.Where("user_id = ?", c.GetInt("id"))
	var total int64
	if err := query.Model(&model.InvoiceApplication{}).Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err := query.Order("id DESC").Offset(page.GetStartIdx()).Limit(page.GetPageSize()).Find(&items).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(items)
	common.ApiSuccess(c, page)
}

func CreateUserInvoice(c *gin.Context) {
	var request model.InvoiceApplication
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32*1024)
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorMsg(c, "invalid invoice request")
		return
	}
	application, err := model.CreateInvoiceApplication(c.GetInt("id"), request)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, application)
}

func GetInvoiceFile(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid invoice id")
		return
	}
	var application model.InvoiceApplication
	query := model.DB.Where("id = ?", id)
	if c.GetInt("role") < common.RoleAdminUser {
		query = query.Where("user_id = ?", c.GetInt("id"))
	}
	if err := query.First(&application).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var file model.InvoiceFile
	if err := model.DB.First(&file, id).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	name := strings.TrimSpace(application.FileName)
	if name == "" {
		name = "invoice.pdf"
	}
	name = filepath.Base(strings.ReplaceAll(strings.ReplaceAll(name, "\r", ""), "\n", ""))
	c.Header("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(name, `"`, "")+`"`)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, file.ContentType, file.Data)
}

func AdminListInvoices(c *gin.Context) {
	page := common.GetPageQuery(c)
	query := model.DB.Model(&model.InvoiceApplication{})
	if userID, err := strconv.Atoi(c.Query("user_id")); err == nil && userID > 0 {
		query = query.Where("user_id = ?", userID)
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var items []model.InvoiceApplication
	if err := query.Order("id DESC").Offset(page.GetStartIdx()).Limit(page.GetPageSize()).Find(&items).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(items)
	common.ApiSuccess(c, page)
}

func AdminReviewInvoice(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid invoice id")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(model.MaxInvoiceFileSize)+64*1024)
	if err := c.Request.ParseMultipartForm(model.MaxInvoiceFileSize + 64*1024); err != nil && !errors.Is(err, multipart.ErrMessageTooLarge) {
		common.ApiErrorMsg(c, "invalid multipart request")
		return
	}
	status := strings.TrimSpace(c.PostForm("status"))
	note := strings.TrimSpace(c.PostForm("note"))
	taxPaid := c.PostForm("tax_paid") == "true" || c.PostForm("tax_paid") == "1"
	var invoiceFile *model.InvoiceFile
	fileName := ""
	if header, fileErr := c.FormFile("file"); fileErr == nil {
		if header.Size <= 0 || header.Size > model.MaxInvoiceFileSize {
			common.ApiErrorMsg(c, "invoice file is too large")
			return
		}
		opened, openErr := header.Open()
		if openErr != nil {
			common.ApiError(c, openErr)
			return
		}
		defer opened.Close()
		data := make([]byte, header.Size)
		if _, readErr := io.ReadFull(opened, data); readErr != nil {
			common.ApiError(c, readErr)
			return
		}
		invoiceFile = &model.InvoiceFile{ContentType: header.Header.Get("Content-Type"), Data: data}
		fileName = filepath.Base(header.Filename)
	}
	if status == model.InvoiceApproved && invoiceFile == nil {
		common.ApiErrorMsg(c, "approved invoices require a file")
		return
	}
	if err := model.ReviewInvoiceApplication(id, c.GetInt("id"), status, note, fileName, invoiceFile, taxPaid); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

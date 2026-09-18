package model

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const (
	InvoiceTypeGeneral          = "general"
	InvoiceTypeSpecial          = "special"
	InvoicePending              = "pending"
	InvoiceApproved             = "approved"
	InvoiceRejected             = "rejected"
	MaxInvoiceAmountCents int64 = 1000000000000
	MaxInvoiceFileSize          = 5 * 1024 * 1024
)

// InvoiceApplication snapshots the invoice details and delivery address at submission.
type InvoiceApplication struct {
	ID               int    `json:"id"`
	UserID           int    `json:"user_id" gorm:"index"`
	Type             string `json:"type" gorm:"type:varchar(16)"`
	AmountCents      int64  `json:"amount_cents"`
	TaxRate          int    `json:"tax_rate"`
	ExtraTaxCents    int64  `json:"extra_tax_cents"`
	TaxPaidConfirmed bool   `json:"tax_paid_confirmed"`
	Title            string `json:"title" gorm:"type:varchar(255)"`
	TaxNumber        string `json:"tax_number" gorm:"type:varchar(64)"`
	Address          string `json:"address" gorm:"type:varchar(500)"`
	Phone            string `json:"phone" gorm:"type:varchar(64)"`
	BankName         string `json:"bank_name" gorm:"type:varchar(255)"`
	BankAccount      string `json:"bank_account" gorm:"type:varchar(128)"`
	Email            string `json:"email" gorm:"type:varchar(320)"`
	Status           string `json:"status" gorm:"type:varchar(16);index"`
	FileName         string `json:"file_name" gorm:"type:varchar(255)"`
	AdminID          int    `json:"admin_id"`
	AdminNote        string `json:"admin_note" gorm:"type:text"`
	CreatedAt        int64  `json:"created_at"`
	ReviewedAt       int64  `json:"reviewed_at"`
}

// Keep file data outside list queries. GORM maps this to longblob/bytea/blob.
type InvoiceFile struct {
	ID          int    `gorm:"primaryKey;autoIncrement:false"`
	ContentType string `gorm:"type:varchar(64)"`
	Data        []byte `gorm:"size:5242880"`
}

type InvoiceSummary struct {
	TopUpAmountCents     int64 `json:"topup_amount_cents"`
	InvoicedAmountCents  int64 `json:"invoiced_amount_cents"`
	PendingAmountCents   int64 `json:"pending_amount_cents"`
	AvailableAmountCents int64 `json:"available_amount_cents"`
	UnverifiedOrders     int   `json:"unverified_orders"`
}

func InvoiceSummaryForUser(tx *gorm.DB, userID int) (InvoiceSummary, error) {
	var summary InvoiceSummary
	var orders []TopUp
	if err := tx.Select("money", "payment_provider", "payment_method", "invoice_amount_cents").
		Where("user_id = ? AND status = ? AND (is_gift = ? OR is_gift IS NULL)", userID, common.TopUpStatusSuccess, false).Find(&orders).Error; err != nil {
		return summary, err
	}
	for _, order := range orders {
		cents := order.InvoiceAmountCents
		// Epay settles in CNY. Other legacy providers may settle in USD; never
		// silently reinterpret their money or quota as renminbi.
		if cents == nil && (order.PaymentProvider == PaymentProviderEpay || order.PaymentProvider == "" && (order.PaymentMethod == "alipay" || order.PaymentMethod == "wxpay" || order.PaymentMethod == "qqpay")) {
			amount := decimal.NewFromFloat(order.Money).Mul(decimal.NewFromInt(100)).Round(0)
			if amount.IsNegative() || amount.GreaterThan(decimal.NewFromInt(MaxInvoiceAmountCents)) {
				return summary, errors.New("invalid historical payment amount")
			}
			value := amount.IntPart()
			cents = &value
		}
		if cents == nil {
			summary.UnverifiedOrders++
			continue
		}
		if *cents < 0 || *cents > MaxInvoiceAmountCents || summary.TopUpAmountCents > int64(common.MaxWalletQuota)-*cents {
			return summary, errors.New("invoice total exceeds supported range")
		}
		summary.TopUpAmountCents += *cents
	}
	if err := tx.Model(&InvoiceApplication{}).Where("user_id = ? AND status = ?", userID, InvoiceApproved).Select("COALESCE(SUM(amount_cents), 0)").Scan(&summary.InvoicedAmountCents).Error; err != nil {
		return summary, err
	}
	if err := tx.Model(&InvoiceApplication{}).Where("user_id = ? AND status = ?", userID, InvoicePending).Select("COALESCE(SUM(amount_cents), 0)").Scan(&summary.PendingAmountCents).Error; err != nil {
		return summary, err
	}
	summary.AvailableAmountCents = max(0, summary.TopUpAmountCents-summary.InvoicedAmountCents-summary.PendingAmountCents)
	return summary, nil
}

func CreateInvoiceApplication(userID int, request InvoiceApplication) (*InvoiceApplication, error) {
	if !setting.GetInvoiceSetting().Allows(userID) {
		return nil, errors.New("invoice service is not available")
	}
	if (request.Type != InvoiceTypeGeneral && request.Type != InvoiceTypeSpecial) || request.AmountCents <= 0 || request.AmountCents > MaxInvoiceAmountCents {
		return nil, errors.New("invalid invoice application")
	}
	request.Title = strings.TrimSpace(request.Title)
	request.TaxNumber = strings.TrimSpace(request.TaxNumber)
	if request.Title == "" || len(request.Title) > 255 || len(request.TaxNumber) > 64 || len(request.Address) > 500 || len(request.Phone) > 64 || len(request.BankName) > 255 || len(request.BankAccount) > 128 {
		return nil, errors.New("invalid invoice details")
	}
	if request.Type == InvoiceTypeSpecial && (request.TaxNumber == "" || strings.TrimSpace(request.Address) == "" || strings.TrimSpace(request.Phone) == "" || strings.TrimSpace(request.BankName) == "" || strings.TrimSpace(request.BankAccount) == "") {
		return nil, errors.New("special invoices require complete company, tax and bank details")
	}
	application := InvoiceApplication{UserID: userID, Type: request.Type, AmountCents: request.AmountCents, Title: request.Title, TaxNumber: request.TaxNumber, Address: request.Address, Phone: request.Phone, BankName: request.BankName, BankAccount: request.BankAccount, Email: strings.TrimSpace(request.Email), Status: InvoicePending, TaxRate: 1, CreatedAt: common.GetTimestamp()}
	if application.Type == InvoiceTypeSpecial {
		application.TaxRate = 6
		application.ExtraTaxCents = (application.AmountCents + 10) / 20
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).Select("id", "email").First(&user, userID).Error; err != nil {
			return err
		}
		if application.Email == "" {
			application.Email = strings.TrimSpace(user.Email)
		}
		address, err := mail.ParseAddress(application.Email)
		if err != nil || address.Address != application.Email || len(application.Email) > 320 || strings.ContainsAny(application.Email, "\r\n;") {
			return errors.New("invalid invoice email")
		}
		summary, err := InvoiceSummaryForUser(tx, userID)
		if err != nil {
			return err
		}
		if application.AmountCents > summary.AvailableAmountCents {
			return errors.New("invoice amount exceeds available amount")
		}
		return tx.Create(&application).Error
	})
	return &application, err
}

func ReviewInvoiceApplication(id, adminID int, status, note, fileName string, file *InvoiceFile, taxPaid bool) error {
	if status != InvoiceApproved && status != InvoiceRejected || len(note) > 2000 {
		return errors.New("invalid invoice review")
	}
	if status == InvoiceApproved && (file == nil || len(file.Data) == 0 || len(file.Data) > MaxInvoiceFileSize || fileName == "" || len(fileName) > 255) {
		return errors.New("an invoice file is required")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var application InvoiceApplication
		if err := lockForUpdate(tx).First(&application, id).Error; err != nil {
			return err
		}
		if application.Status != InvoicePending {
			return errors.New("invoice has already been reviewed")
		}
		if status == InvoiceApproved && application.Type == InvoiceTypeSpecial && !taxPaid {
			return errors.New("confirm offline collection of the additional 5% tax before issuing the invoice")
		}
		application.Status, application.AdminID, application.AdminNote, application.ReviewedAt = status, adminID, note, common.GetTimestamp()
		if status == InvoiceApproved {
			application.TaxPaidConfirmed = taxPaid && application.Type == InvoiceTypeSpecial
			application.FileName = fileName
			file.ID = id
			if err := tx.Create(file).Error; err != nil {
				return err
			}
			message := EmailNotification{EventKey: fmt.Sprintf("invoice:%d", id), UserID: application.UserID, Email: application.Email, Subject: "您的技术服务费发票已开具", Content: "<p>您好，您的技术服务费发票已开具，请查收附件。您也可以登录控制台下载发票。</p>", Status: "pending", CreatedAt: common.GetTimestamp(), InvoiceID: id}
			if err := tx.Create(&message).Error; err != nil {
				return err
			}
		}
		return tx.Save(&application).Error
	})
}

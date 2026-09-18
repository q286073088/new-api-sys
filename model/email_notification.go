package model

import (
	"context"
	"errors"
	"html"
	"net/mail"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EmailNotification is an outbox written in the same transaction as a payment
// or reward. SMTP failures never undo the credited balance.
type EmailNotification struct {
	ID          int
	EventKey    string `gorm:"type:varchar(160);uniqueIndex"`
	UserID      int
	InvoiceID   int    `gorm:"index"`
	Email       string `gorm:"type:varchar(320)"`
	Subject     string `gorm:"type:varchar(255)"`
	Content     string `gorm:"type:text"`
	Status      string `gorm:"type:varchar(16);index:idx_email_due"`
	Attempts    int
	AvailableAt int64  `gorm:"index:idx_email_due"`
	ClaimID     string `gorm:"type:varchar(64)"`
	CreatedAt   int64
	SentAt      int64
}

func QueueUserEmail(tx *gorm.DB, eventKey string, userID int, subject, content string) (bool, error) {
	var user User
	if err := tx.Select("id", "email").First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	address := strings.TrimSpace(user.Email)
	parsed, err := mail.ParseAddress(address)
	if err != nil || parsed.Address != address || strings.ContainsAny(address, "\r\n;") {
		return false, nil
	}
	message := EmailNotification{
		EventKey: eventKey, UserID: userID, Email: address, Subject: subject,
		Content: "<p>" + strings.ReplaceAll(html.EscapeString(content), "\n", "<br>\n") + "</p>",
		Status:  "pending", CreatedAt: common.GetTimestamp(),
	}
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&message)
	return result.RowsAffected == 1, result.Error
}

type EmailDeliverySummary struct {
	Sent    int `json:"sent"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

func HasDueEmailNotifications() bool {
	var count int64
	DB.Model(&EmailNotification{}).Where("status IN ? AND available_at <= ?", []string{"pending", "sending"}, common.GetTimestamp()).Limit(1).Count(&count)
	return count > 0
}

// DispatchEmailNotifications claims each row before delivery. A lease allows
// another worker to resume after a crash without duplicate live deliveries.
func DispatchEmailNotifications(ctx context.Context, now int64, send func(subject, receiver, content string) error) (EmailDeliverySummary, error) {
	return dispatchEmailNotifications(ctx, now, send, nil)
}

func DispatchEmailNotificationsWithAttachments(ctx context.Context, now int64, send func(subject, receiver, content string) error, sendWithAttachments func(subject, receiver, content string, attachments []common.EmailAttachment) error) (EmailDeliverySummary, error) {
	return dispatchEmailNotifications(ctx, now, send, sendWithAttachments)
}

func dispatchEmailNotifications(ctx context.Context, now int64, send func(subject, receiver, content string) error, sendWithAttachments func(subject, receiver, content string, attachments []common.EmailAttachment) error) (EmailDeliverySummary, error) {
	summary := EmailDeliverySummary{}
	var messages []EmailNotification
	if err := DB.WithContext(ctx).Where("status IN ? AND available_at <= ?", []string{"pending", "sending"}, now).
		Order("id").Limit(100).Find(&messages).Error; err != nil {
		return summary, err
	}
	for _, message := range messages {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		claimID := common.GetUUID()
		claimTime := max(now, common.GetTimestamp())
		result := DB.WithContext(ctx).Model(&EmailNotification{}).
			Where("id = ? AND status IN ? AND available_at <= ?", message.ID, []string{"pending", "sending"}, now).
			Updates(map[string]any{"status": "sending", "claim_id": claimID, "available_at": claimTime + 300, "attempts": gorm.Expr("attempts + 1")})
		if result.Error != nil {
			return summary, result.Error
		}
		if result.RowsAffected == 0 {
			continue
		}
		status, sentAt := "sent", common.GetTimestamp()
		var user User
		err := DB.WithContext(ctx).Select("id", "email").First(&user, message.UserID).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return summary, err
		}
		// Invoice emails intentionally use the address entered on the application;
		// all other notifications must still follow the user's current address.
		if errors.Is(err, gorm.ErrRecordNotFound) || (message.InvoiceID == 0 && strings.TrimSpace(user.Email) != message.Email) {
			status, sentAt = "skipped", 0
			summary.Skipped++
		} else {
			var deliveryErr error
			if message.InvoiceID > 0 && sendWithAttachments != nil {
				var file InvoiceFile
				if fileErr := DB.WithContext(ctx).First(&file, message.InvoiceID).Error; fileErr != nil {
					deliveryErr = fileErr
				} else {
					filename := "invoice.pdf"
					var application InvoiceApplication
					if appErr := DB.WithContext(ctx).Select("file_name").First(&application, message.InvoiceID).Error; appErr == nil && strings.TrimSpace(application.FileName) != "" {
						filename = strings.TrimSpace(application.FileName)
					}
					deliveryErr = sendWithAttachments(message.Subject, message.Email, message.Content, []common.EmailAttachment{{Filename: filename, ContentType: file.ContentType, Data: file.Data}})
				}
			} else {
				deliveryErr = send(message.Subject, message.Email, message.Content)
			}
			if deliveryErr != nil {
				status, sentAt = "pending", 0
				if message.Attempts+1 >= 5 {
					status = "failed"
				}
				summary.Failed++
			} else {
				summary.Sent++
			}
		}
		// Delivery may finish while the scheduler's context is cancelled. Persist
		// the SMTP acknowledgement instead of sending the accepted message again.
		result = DB.Model(&EmailNotification{}).Where("id = ? AND claim_id = ? AND status = ?", message.ID, claimID, "sending").
			Updates(map[string]any{"status": status, "sent_at": sentAt, "available_at": claimTime + int64(min(message.Attempts+1, 5))*300, "claim_id": ""})
		if result.Error != nil {
			return summary, result.Error
		}
		if result.RowsAffected != 1 {
			return summary, errors.New("email delivery lease lost")
		}
	}
	if summary.Failed > 0 {
		return summary, errors.New("some emails could not be delivered; see delivery counts and SMTP settings")
	}
	return summary, nil
}

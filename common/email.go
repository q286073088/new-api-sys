package common

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/smtp"
	"slices"
	"strings"
	"time"
)

// EmailAttachment is an attachment included in an outgoing HTML email.
type EmailAttachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

func generateMessageID() (string, error) {
	split := strings.Split(SMTPFrom, "@")
	if len(split) < 2 {
		return "", fmt.Errorf("invalid SMTP account")
	}
	domain := strings.Split(SMTPFrom, "@")[1]
	return fmt.Sprintf("<%d.%s@%s>", time.Now().UnixNano(), GetRandomString(12), domain), nil
}

func shouldUseSMTPLoginAuth() bool {
	if SMTPForceAuthLogin {
		return true
	}
	return isOutlookServer(SMTPAccount) || slices.Contains(EmailLoginAuthServerList, SMTPServer)
}

func getSMTPAuth() smtp.Auth {
	return AutoSMTPAuth(SMTPAccount, SMTPToken)
}

func shouldAuthenticateSMTP() bool {
	return SMTPAccount != "" && SMTPToken != ""
}

func smtpTLSConfig() *tls.Config {
	return &tls.Config{
		ServerName:         SMTPServer,
		InsecureSkipVerify: SMTPInsecureSkipVerify, // #nosec G402 -- admin-controlled SMTP compatibility option.
	}
}

func newSMTPClient(addr string) (*smtp.Client, error) {
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	if SMTPSSLEnabled || (SMTPPort == 465 && !SMTPStartTLSEnabled) {
		conn, err := tls.DialWithDialer(dialer, "tcp", addr, smtpTLSConfig())
		if err != nil {
			return nil, err
		}
		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
		client, err := smtp.NewClient(conn, SMTPServer)
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		return client, nil
	}

	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	client, err := smtp.NewClient(conn, SMTPServer)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	if SMTPStartTLSEnabled {
		startTLSSupported, _ := client.Extension("STARTTLS")
		if !startTLSSupported {
			_ = client.Close()
			return nil, fmt.Errorf("SMTP server does not support STARTTLS")
		}
		if err := client.StartTLS(smtpTLSConfig()); err != nil {
			_ = client.Close()
			return nil, err
		}
	}

	return client, nil
}

func SendEmail(subject string, receiver string, content string) error {
	return SendEmailWithAttachments(subject, receiver, content, nil)
}

func SendEmailWithAttachments(subject string, receiver string, content string, attachments []EmailAttachment) error {
	if SMTPFrom == "" { // for compatibility
		SMTPFrom = SMTPAccount
	}
	id, err2 := generateMessageID()
	if err2 != nil {
		return err2
	}
	if SMTPServer == "" && SMTPAccount == "" {
		return fmt.Errorf("SMTP 服务器未配置")
	}
	encodedSubject := fmt.Sprintf("=?UTF-8?B?%s?=", base64.StdEncoding.EncodeToString([]byte(subject)))
	var body bytes.Buffer
	body.WriteString(fmt.Sprintf("To: %s\r\nFrom: %s <%s>\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: %s\r\n",
		receiver, SystemName, SMTPFrom, encodedSubject, time.Now().Format(time.RFC1123Z), id))
	if len(attachments) == 0 {
		body.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
		body.WriteString(content)
		body.WriteString("\r\n")
	} else {
		boundary := "newapi-" + GetRandomString(24)
		body.WriteString(fmt.Sprintf("MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", boundary))
		body.WriteString("--" + boundary + "\r\nContent-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
		body.WriteString(content)
		body.WriteString("\r\n")
		for _, attachment := range attachments {
			if len(attachment.Data) == 0 || attachment.Filename == "" {
				continue
			}
			contentType := attachment.ContentType
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			body.WriteString(fmt.Sprintf("--%s\r\nContent-Type: %s\r\nContent-Disposition: attachment; filename=\"%s\"\r\nContent-Transfer-Encoding: base64\r\n\r\n", boundary, contentType, strings.ReplaceAll(strings.ReplaceAll(attachment.Filename, "\"", ""), "\r", "")))
			encoded := make([]byte, base64.StdEncoding.EncodedLen(len(attachment.Data)))
			base64.StdEncoding.Encode(encoded, attachment.Data)
			for len(encoded) > 76 {
				body.Write(encoded[:76])
				body.WriteString("\r\n")
				encoded = encoded[76:]
			}
			body.Write(encoded)
			body.WriteString("\r\n")
		}
		body.WriteString("--" + boundary + "--\r\n")
	}
	mail := body.Bytes()
	auth := getSMTPAuth()
	addr := fmt.Sprintf("%s:%d", SMTPServer, SMTPPort)
	to := strings.Split(receiver, ";")
	var err error
	client, err := newSMTPClient(addr)
	if err != nil {
		return err
	}
	defer client.Close()
	if shouldAuthenticateSMTP() {
		if err = client.Auth(auth); err != nil {
			return err
		}
	}
	if err = client.Mail(SMTPFrom); err != nil {
		return err
	}
	for _, receiver := range to {
		if err = client.Rcpt(receiver); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	_, err = w.Write(mail)
	if err != nil {
		return err
	}
	err = w.Close()
	if err != nil {
		return err
	}
	err = client.Quit()
	if err != nil {
		SysError(fmt.Sprintf("email accepted for %s but SMTP QUIT failed: %v", receiver, err))
	}
	// DATA was acknowledged. A failed QUIT must not resend an accepted email.
	return nil
}

package output

import (
	"crypto/tls"
	"fmt"
	"os"
	"strings"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/model"
	"gopkg.in/gomail.v2"
)

func SendEmail(briefing *model.Briefing, cfg *config.Config, failed []fetcher.FailedSource) error {
	password := os.Getenv("EMAIL_PASSWORD")
	if password == "" {
		return fmt.Errorf("EMAIL_PASSWORD not set in .env")
	}

	subject := briefingEmailSubject(briefing.Date, briefing.Period)
	body := buildEmailBody(briefing, failed)

	m := gomail.NewMessage()
	m.SetHeader("From", cfg.Email.From)
	m.SetHeader("To", cfg.Email.To)
	m.SetHeader("Subject", subject)
	m.SetBody("text/plain", body)

	d := gomail.NewDialer(cfg.Email.SMTPHost, cfg.Email.SMTPPort, cfg.Email.From, password)
	d.TLSConfig = &tls.Config{ServerName: cfg.Email.SMTPHost}

	if err := d.DialAndSend(m); err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	return nil
}

func buildEmailBody(briefing *model.Briefing, failed []fetcher.FailedSource) string {
	header := briefingTitle(briefing.Date, briefing.Period) + "\n\n"
	body := header + briefing.RawContent
	if len(failed) == 0 {
		return body
	}

	var tail strings.Builder
	tail.WriteString("\n\n---\n抓取异常\n")
	for _, f := range failed {
		tail.WriteString(fmt.Sprintf("- %s: %v\n", f.Name, f.Err))
	}
	return body + tail.String()
}

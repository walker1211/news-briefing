package output

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/model"
	"golang.org/x/net/proxy"
	"gopkg.in/gomail.v2"
)

var briefingMarkdownPattern = regexp.MustCompile(`^(\d{2}\.\d{2}\.\d{2})-(凌晨|早间|午间|晚间)-(\d{4})\.md$`)

type smtpSendFunc func(*config.Config, string, string, string) error
type directEmailDialContextFactory func(time.Duration) func(context.Context, string, string) (net.Conn, error)
type socks5EmailDialContextFactory func(string, time.Duration) (func(context.Context, string, string) (net.Conn, error), error)

type EmailSender struct {
	smtpSend                  smtpSendFunc
	newDirectEmailDialContext directEmailDialContextFactory
	newSocks5EmailDialContext socks5EmailDialContextFactory
	sleep                     func(time.Duration)
}

func NewEmailSender() *EmailSender {
	return &EmailSender{
		newDirectEmailDialContext: defaultDirectEmailDialContext,
		newSocks5EmailDialContext: defaultSocks5EmailDialContext,
		sleep:                     time.Sleep,
	}
}

func defaultDirectEmailDialContext(timeout time.Duration) func(context.Context, string, string) (net.Conn, error) {
	baseDialer := &net.Dialer{Timeout: timeout}
	return baseDialer.DialContext
}

func defaultSocks5EmailDialContext(proxyAddr string, timeout time.Duration) (func(context.Context, string, string) (net.Conn, error), error) {
	parsed, err := url.Parse(proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("parse proxy.socks5: %w", err)
	}
	baseDialer := &net.Dialer{Timeout: timeout}
	dialer, err := proxy.FromURL(parsed, baseDialer)
	if err != nil {
		return nil, fmt.Errorf("build proxy dialer: %w", err)
	}
	if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
		return contextDialer.DialContext, nil
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return proxy.Dial(ctx, network, address)
	}, nil
}

func newEmailDialContext(cfg *config.Config) (func(context.Context, string, string) (net.Conn, error), error) {
	return NewEmailSender().newEmailDialContext(cfg)
}

func (s *EmailSender) newEmailDialContext(cfg *config.Config) (func(context.Context, string, string) (net.Conn, error), error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if !cfg.Email.UseProxy {
		factory := s.newDirectEmailDialContext
		if factory == nil {
			factory = defaultDirectEmailDialContext
		}
		return factory(cfg.Email.Timeout), nil
	}
	proxyAddr := strings.TrimSpace(cfg.Proxy.Socks5)
	if proxyAddr == "" {
		return nil, fmt.Errorf("email.use_proxy requires proxy.socks5")
	}
	factory := s.newSocks5EmailDialContext
	if factory == nil {
		factory = defaultSocks5EmailDialContext
	}
	return factory(proxyAddr, cfg.Email.Timeout)
}

func SendEmail(briefing *model.Briefing, cfg *config.Config, failed []fetcher.FailedSource) error {
	return NewEmailSender().SendEmail(briefing, cfg, failed)
}

func (s *EmailSender) SendEmail(briefing *model.Briefing, cfg *config.Config, failed []fetcher.FailedSource) error {
	if err := validateEmailInputs(briefing, cfg); err != nil {
		return err
	}
	return s.sendEmailWithContent(cfg, briefingEmailSubject(briefing.Date, briefing.Period), buildEmailBody(briefing, failed))
}

func SendDeepEmail(topic string, briefing *model.Briefing, cfg *config.Config, failed []fetcher.FailedSource) error {
	return NewEmailSender().SendDeepEmail(topic, briefing, cfg, failed)
}

func (s *EmailSender) SendDeepEmail(topic string, briefing *model.Briefing, cfg *config.Config, failed []fetcher.FailedSource) error {
	if err := validateEmailInputs(briefing, cfg); err != nil {
		return err
	}
	return s.sendEmailWithContent(cfg, deepEmailSubject(topic), buildDeepEmailBody(topic, briefing, failed))
}

func validateEmailInputs(briefing *model.Briefing, cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if briefing == nil {
		return fmt.Errorf("briefing is nil")
	}
	return nil
}

func SendMarkdownFile(path string, cfg *config.Config) error {
	return NewEmailSender().SendMarkdownFile(path, cfg)
}

func (s *EmailSender) SendMarkdownFile(path string, cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	cleanPath := filepath.Clean(path)
	if !strings.HasSuffix(strings.ToLower(cleanPath), ".md") {
		return fmt.Errorf("markdown file must end with .md")
	}
	outputDir, err := filepath.Abs(cfg.Output.Dir)
	if err != nil {
		return fmt.Errorf("resolve output dir: %w", err)
	}
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return fmt.Errorf("resolve markdown file: %w", err)
	}
	relInput, err := filepath.Rel(outputDir, absPath)
	if err != nil {
		return fmt.Errorf("check markdown path: %w", err)
	}
	if relInput == ".." || strings.HasPrefix(relInput, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("markdown file %q is outside output dir %q", cleanPath, cfg.Output.Dir)
	}

	realOutputDir, err := filepath.EvalSymlinks(outputDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("resolve output dir symlink: %w", err)
		}
		realOutputDir = outputDir
	}
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("resolve markdown file symlink: %w", err)
		}
		realPath = absPath
	}
	relReal, err := filepath.Rel(realOutputDir, realPath)
	if err != nil {
		return fmt.Errorf("check markdown path: %w", err)
	}
	if relReal == ".." || strings.HasPrefix(relReal, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("markdown file %q is outside output dir %q", cleanPath, cfg.Output.Dir)
	}

	subject, err := briefingSubjectFromMarkdownFilename(realPath)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("read markdown file: %w", err)
	}
	return s.sendEmailWithContent(cfg, subject, string(data))
}

func briefingSubjectFromMarkdownFilename(path string) (string, error) {
	base := filepath.Base(path)
	matches := briefingMarkdownPattern.FindStringSubmatch(base)
	if len(matches) != 4 {
		return "", fmt.Errorf("parse markdown filename %q: expected YY.MM.DD-<凌晨|早间|午间|晚间>-HHMM.md", base)
	}
	return briefingTitle(matches[1], matches[3]), nil
}

func (s *EmailSender) sendEmailWithContent(cfg *config.Config, subject string, body string) error {
	password := os.Getenv("EMAIL_SMTP_AUTH_CODE")
	if password == "" {
		return fmt.Errorf("EMAIL_SMTP_AUTH_CODE not set in .env")
	}
	send := s.smtpSend
	if send == nil {
		send = s.deliverSMTPMessage
	}
	sleep := s.sleep
	if sleep == nil {
		sleep = time.Sleep
	}

	var lastErr error
	for attempt := 1; attempt <= cfg.Email.RetryTimes; attempt++ {
		if err := send(cfg, subject, body, password); err != nil {
			lastErr = err
			if attempt < cfg.Email.RetryTimes {
				sleep(cfg.Email.RetryWaitTime)
				continue
			}
			break
		}
		return nil
	}

	return fmt.Errorf("send email after %d attempts: %w", cfg.Email.RetryTimes, lastErr)
}

func (s *EmailSender) deliverSMTPMessage(cfg *config.Config, subject string, body string, password string) error {
	m := gomail.NewMessage()
	m.SetHeader("From", cfg.Email.From)
	m.SetHeader("To", cfg.Email.To)
	m.SetHeader("Subject", subject)
	m.SetBody("text/plain", body)

	dialContext, err := s.newEmailDialContext(cfg)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Email.Timeout)
	defer cancel()
	conn, err := dialContext(ctx, "tcp", fmt.Sprintf("%s:%d", cfg.Email.SMTPHost, cfg.Email.SMTPPort))
	if err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(cfg.Email.Timeout))

	tlsConn := tls.Client(conn, &tls.Config{ServerName: cfg.Email.SMTPHost})
	if err := tlsConn.Handshake(); err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	defer tlsConn.Close()
	_ = tlsConn.SetDeadline(time.Now().Add(cfg.Email.Timeout))

	client, err := smtp.NewClient(tlsConn, cfg.Email.SMTPHost)
	if err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	defer client.Close()

	if err := client.Hello("localhost"); err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	auth := smtp.PlainAuth("", cfg.Email.From, password, cfg.Email.SMTPHost)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	if err := client.Mail(cfg.Email.From); err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	if err := client.Rcpt(cfg.Email.To); err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	var rendered bytes.Buffer
	if _, err := m.WriteTo(&rendered); err != nil {
		_ = writer.Close()
		return fmt.Errorf("send email: %w", err)
	}
	if _, err := writer.Write(rendered.Bytes()); err != nil {
		_ = writer.Close()
		return fmt.Errorf("send email: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	return nil
}

func buildEmailBody(briefing *model.Briefing, failed []fetcher.FailedSource) string {
	header := briefingTitle(briefing.Date, briefing.Period) + "\n\n"
	return appendFailedSection(header+briefing.RawContent, failed)
}

func buildDeepEmailBody(topic string, briefing *model.Briefing, failed []fetcher.FailedSource) string {
	header := deepEmailTitle(topic) + "\n\n"
	return appendFailedSection(header+briefing.RawContent, failed)
}

func appendFailedSection(body string, failed []fetcher.FailedSource) string {
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

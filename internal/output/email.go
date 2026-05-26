package output

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"html"
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
var htmlURLPattern = regexp.MustCompile(`https?://[A-Za-z0-9._~:/?#\[\]@!$&'()*+,;=%-]+`)

type smtpSendFunc func(*config.Config, string, string, string) error
type smtpHTMLSendFunc func(*config.Config, string, string, string, string) error
type directEmailDialContextFactory func(time.Duration) func(context.Context, string, string) (net.Conn, error)
type socks5EmailDialContextFactory func(string, time.Duration) (func(context.Context, string, string) (net.Conn, error), error)

type EmailSender struct {
	smtpSend                  smtpSendFunc
	smtpHTMLSend              smtpHTMLSendFunc
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

func ValidateEmailReadyForSending(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if err := config.ValidateEmailForSending(cfg.Email); err != nil {
		return err
	}
	if os.Getenv("EMAIL_SMTP_AUTH_CODE") == "" {
		return fmt.Errorf("EMAIL_SMTP_AUTH_CODE not set in .env")
	}
	if cfg.Email.UseProxy && strings.TrimSpace(cfg.Proxy.Socks5) == "" {
		return fmt.Errorf("email.use_proxy requires proxy.socks5")
	}
	return nil
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
	return s.sendHTMLEmailWithContent(cfg, briefingEmailSubject(briefing.Date, briefing.Period), buildEmailBody(briefing, failed))
}

func SendDeepEmail(topic string, briefing *model.Briefing, cfg *config.Config, failed []fetcher.FailedSource) error {
	return NewEmailSender().SendDeepEmail(topic, briefing, cfg, failed)
}

func (s *EmailSender) SendDeepEmail(topic string, briefing *model.Briefing, cfg *config.Config, failed []fetcher.FailedSource) error {
	if err := validateEmailInputs(briefing, cfg); err != nil {
		return err
	}
	return s.sendHTMLEmailWithContent(cfg, deepEmailSubject(topic), buildDeepEmailBody(topic, briefing, failed))
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
	return s.sendHTMLEmailWithContent(cfg, subject, string(data))
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

func (s *EmailSender) sendHTMLEmailWithContent(cfg *config.Config, subject string, body string) error {
	password := os.Getenv("EMAIL_SMTP_AUTH_CODE")
	if password == "" {
		return fmt.Errorf("EMAIL_SMTP_AUTH_CODE not set in .env")
	}
	send := s.smtpHTMLSend
	if send == nil {
		send = s.deliverSMTPHTMLMessage
	}
	sleep := s.sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	htmlBody := buildHTMLBody(body)

	var lastErr error
	for attempt := 1; attempt <= cfg.Email.RetryTimes; attempt++ {
		if err := send(cfg, subject, body, htmlBody, password); err != nil {
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
	return s.deliverMessage(cfg, m, password)
}

func (s *EmailSender) deliverSMTPHTMLMessage(cfg *config.Config, subject string, textBody string, htmlBody string, password string) error {
	m := gomail.NewMessage()
	m.SetHeader("From", cfg.Email.From)
	m.SetHeader("To", cfg.Email.To)
	m.SetHeader("Subject", subject)
	m.SetBody("text/plain", textBody)
	m.AddAlternative("text/html", htmlBody)
	return s.deliverMessage(cfg, m, password)
}

func (s *EmailSender) deliverMessage(cfg *config.Config, m *gomail.Message, password string) error {
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

func buildHTMLBody(body string) string {
	return `<!doctype html>
<html>
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
body{margin:0;padding:0;background:#f6f7f9;color:#172033;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Helvetica Neue",Arial,"PingFang SC","Hiragino Sans GB","Microsoft YaHei",sans-serif;line-height:1.65;overflow-wrap:anywhere;word-break:break-word;}
.newsletter-shell{max-width:720px;margin:0 auto;padding:24px 14px;}
.newsletter-card{background:#fff;border:1px solid #e6e8ee;border-radius:18px;padding:28px 30px;box-shadow:0 10px 30px rgba(23,32,51,.06);}
h1{margin:0 0 24px;font-size:24px;line-height:1.3;color:#111827;letter-spacing:-.02em;}
.briefing-section{margin:28px 0 0;padding:0;}.briefing-section:first-of-type{margin-top:0}.briefing-section h2{margin:0 0 14px;padding-top:18px;border-top:1px solid #edf0f5;font-size:17px;color:#1f2937;}.briefing-section:first-of-type h2{padding-top:0;border-top:0}.briefing-section h2 span{margin-left:8px;font-size:13px;font-weight:500;color:#6b7280;}
.section-news h2{padding-top:0;border-top:0;color:#2563eb;border-left:4px solid #60a5fa;padding-left:10px;}
.section-status,.section-follow{padding:16px 18px;border:1px solid #eef2f7;border-radius:14px;background:#f8fafc;}.section-status h2,.section-follow h2{padding-top:0;border-top:0;color:#0f766e;}.section-candidates{padding:0;border:0;background:transparent;}.section-candidates h2{padding-top:0;border-top:0;color:#7c3aed;border-left:4px solid #a78bfa;padding-left:10px;}
.warning-block{padding:16px 18px;border:1px solid #fed7aa;border-radius:14px;background:#fff7ed;}.warning-block h2{margin:0 0 10px;padding-top:0;border-top:0;color:#c2410c;}.warning-item{margin:8px 0;color:#7c2d12;}
.news-item{padding:14px 0 18px;border-bottom:1px solid #f0f2f6;}.news-item:last-child{border-bottom:0;padding-bottom:0;}
h3{margin:0 0 8px;font-size:16px;line-height:1.45;color:#111827;}.summary,.impact,.markdown-label{margin:0 0 10px;color:#374151;}.summary strong,.impact strong,.markdown-label strong{color:#111827;}.meta{margin:0 0 10px;color:#6b7280;font-size:13px;}.source-link{display:inline-block;color:#2563eb;text-decoration:none;font-size:13px;font-weight:600;}code{font-family:SFMono-Regular,Consolas,"Liberation Mono",Menlo,monospace;font-size:12px;background:#f3f4f6;border:1px solid #e5e7eb;border-radius:6px;padding:1px 5px;color:#111827;}
.paragraph{margin:0 0 12px;color:#374151;}
@media(max-width:640px){.newsletter-shell{padding:0}.newsletter-card{border-radius:0;border-left:0;border-right:0;padding:20px 18px}h1{font-size:21px}}
</style>
</head>
<body><main class="newsletter-shell"><section class="newsletter-card">` + renderNewsletterHTML(body) + `</section></main></body>
</html>`
}

func renderNewsletterHTML(body string) string {
	var out strings.Builder
	lines := strings.Split(body, "\n")
	inArticle := false
	inSection := false
	inWarningSection := false

	closeArticle := func() {
		if inArticle {
			out.WriteString("</article>")
			inArticle = false
		}
	}
	closeSection := func() {
		closeArticle()
		if inSection {
			out.WriteString("</section>")
			inSection = false
			inWarningSection = false
		}
	}
	openSection := func(title, count string) {
		closeSection()
		sectionClass := htmlSectionClass(title)
		inWarningSection = strings.Contains(sectionClass, "warning-block")
		out.WriteString(`<section class="briefing-section `)
		out.WriteString(sectionClass)
		out.WriteString(`"><h2>`)
		out.WriteString(html.EscapeString(title))
		if count != "" {
			out.WriteString(" <span>")
			out.WriteString(html.EscapeString(count))
			out.WriteString("</span>")
		}
		out.WriteString("</h2>")
		inSection = true
	}

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || line == "---" {
			continue
		}
		if title, ok := strings.CutPrefix(line, "# "); ok {
			closeSection()
			out.WriteString("<h1>")
			out.WriteString(html.EscapeString(strings.TrimSpace(title)))
			out.WriteString("</h1>")
			continue
		}
		if i == 0 {
			closeSection()
			out.WriteString("<h1>")
			out.WriteString(html.EscapeString(line))
			out.WriteString("</h1>")
			continue
		}
		if category, count, ok := parseHTMLCategoryLine(line); ok {
			openSection(category, count)
			continue
		}
		if line == "抓取异常" {
			if !hasRenderableHTMLWarningItems(lines, i+1) {
				continue
			}
			openSection(line, "")
			continue
		}
		if item, ok := strings.CutPrefix(line, "- "); ok && shouldSuppressHTMLWarningItem(item) {
			continue
		}
		if inWarningSection {
			if item, ok := strings.CutPrefix(line, "- "); ok {
				if shouldSuppressHTMLWarningItem(item) {
					continue
				}
				out.WriteString(`<p class="warning-item">`)
				out.WriteString(linkifyHTML(cleanMarkdownLine(item), "链接"))
				out.WriteString("</p>")
				continue
			}
		}
		if title, ok := parseHTMLArticleTitle(line); ok {
			closeArticle()
			out.WriteString(`<article class="news-item"><h3>`)
			out.WriteString(html.EscapeString(title))
			out.WriteString("</h3>")
			inArticle = true
			continue
		}
		if summary, ok := strings.CutPrefix(line, "**摘要：**"); ok {
			out.WriteString(`<p class="summary"><strong>摘要：</strong>`)
			out.WriteString(linkifyHTML(cleanMarkdownLine(summary), "链接"))
			out.WriteString("</p>")
			continue
		}
		if impact, ok := strings.CutPrefix(line, "**影响：**"); ok {
			out.WriteString(`<p class="impact"><strong>影响：</strong>`)
			out.WriteString(linkifyHTML(cleanMarkdownLine(impact), "链接"))
			out.WriteString("</p>")
			continue
		}
		if label, content, ok := parseHTMLStrongLabelLine(line); ok {
			out.WriteString(`<p class="markdown-label"><strong>`)
			out.WriteString(html.EscapeString(label))
			out.WriteString(`</strong>`)
			out.WriteString(renderInlineMarkdownHTML(cleanMarkdownLine(content), "链接"))
			out.WriteString("</p>")
			continue
		}
		if source, ok := strings.CutPrefix(line, "> 来源: "); ok {
			out.WriteString(`<p class="meta">`)
			out.WriteString(html.EscapeString(strings.ReplaceAll(cleanMarkdownLine(source), " | ", " · ")))
			out.WriteString("</p>")
			continue
		}
		if source, ok := strings.CutPrefix(line, "Source: "); ok {
			out.WriteString(`<p class="meta">`)
			out.WriteString(html.EscapeString(strings.ReplaceAll(cleanMarkdownLine(source), " | ", " · ")))
			out.WriteString("</p>")
			continue
		}
		if rawURL, ok := strings.CutPrefix(line, "Link: "); ok {
			out.WriteString(`<a class="source-link" href="`)
			out.WriteString(html.EscapeString(strings.TrimSpace(rawURL)))
			out.WriteString(`">原文链接</a>`)
			continue
		}
		out.WriteString(`<p class="paragraph">`)
		out.WriteString(linkifyHTML(line, "链接"))
		out.WriteString("</p>")
	}
	closeSection()
	return out.String()
}

func htmlSectionClass(title string) string {
	switch strings.TrimSpace(title) {
	case "今日态势":
		return "section-status"
	case "今日最值得追的方向":
		return "section-follow"
	case "未命中关键词的候选新闻":
		return "section-candidates"
	case "Watch 站点异常":
		return "warning-block watch-warning"
	case "抓取异常":
		return "warning-block fetch-warning"
	default:
		return "section-news"
	}
}

func hasRenderableHTMLWarningItems(lines []string, start int) bool {
	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || line == "---" {
			continue
		}
		if strings.HasPrefix(line, "#") || line == "抓取异常" {
			return false
		}
		if _, _, ok := parseHTMLCategoryLine(line); ok {
			return false
		}
		item, ok := strings.CutPrefix(line, "- ")
		if !ok {
			return false
		}
		if !shouldSuppressHTMLWarningItem(item) {
			return true
		}
	}
	return false
}

func shouldSuppressHTMLWarningItem(item string) bool {
	text := strings.TrimSpace(item)
	return strings.HasPrefix(text, "X coverage/search:") && strings.Contains(text, "limit-reached")
}

func parseHTMLCategoryLine(line string) (string, string, bool) {
	if category, ok := strings.CutPrefix(line, "## "); ok {
		return strings.TrimSpace(category), "", true
	}
	if !strings.HasPrefix(line, "== ") || !strings.HasSuffix(line, " ==") {
		return "", "", false
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(line, "== "), " ==")
	if left := strings.LastIndex(inner, "("); left >= 0 && strings.HasSuffix(inner, ")") {
		return strings.TrimSpace(inner[:left]), strings.TrimSuffix(inner[left+1:], ")"), true
	}
	return inner, "", true
}

func parseHTMLArticleTitle(line string) (string, bool) {
	if title, ok := strings.CutPrefix(line, "### "); ok {
		return strings.TrimSpace(title), true
	}
	dot := strings.Index(line, ". ")
	if dot <= 0 {
		return "", false
	}
	for _, r := range line[:dot] {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return strings.TrimSpace(line[dot+2:]), true
}

func cleanMarkdownLine(line string) string {
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), "  "))
}

func parseHTMLStrongLabelLine(line string) (string, string, bool) {
	remainder, ok := strings.CutPrefix(line, "**")
	if !ok {
		return "", "", false
	}
	label, content, ok := strings.Cut(remainder, "**")
	if !ok || strings.TrimSpace(label) == "" {
		return "", "", false
	}
	return strings.TrimSpace(label), content, true
}

func renderInlineMarkdownHTML(body string, label string) string {
	var out strings.Builder
	for {
		start := strings.Index(body, "`")
		if start < 0 {
			out.WriteString(linkifyHTML(body, label))
			return out.String()
		}
		out.WriteString(linkifyHTML(body[:start], label))
		body = body[start+1:]
		end := strings.Index(body, "`")
		if end < 0 {
			out.WriteString(html.EscapeString("`" + body))
			return out.String()
		}
		out.WriteString("<code>")
		out.WriteString(html.EscapeString(body[:end]))
		out.WriteString("</code>")
		body = body[end+1:]
	}
}

func linkifyHTML(body string, label string) string {
	var out strings.Builder
	matches := htmlURLPattern.FindAllStringIndex(body, -1)
	last := 0
	for _, match := range matches {
		start, end := match[0], match[1]
		out.WriteString(html.EscapeString(body[last:start]))
		rawURL := body[start:end]
		out.WriteString(`<a href="`)
		out.WriteString(html.EscapeString(rawURL))
		out.WriteString(`">`)
		out.WriteString(label)
		out.WriteString(`</a>`)
		last = end
	}
	out.WriteString(html.EscapeString(body[last:]))
	return out.String()
}

func buildEmailBody(briefing *model.Briefing, failed []fetcher.FailedSource) string {
	header := briefingTitle(briefing.Date, briefing.Period) + "\n\n"
	return AppendFailedSection(header+briefing.RawContent, failed)
}

func buildDeepEmailBody(topic string, briefing *model.Briefing, failed []fetcher.FailedSource) string {
	header := deepEmailTitle(topic) + "\n\n"
	return AppendFailedSection(header+briefing.RawContent, failed)
}

func AppendFailedSection(body string, failed []fetcher.FailedSource) string {
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

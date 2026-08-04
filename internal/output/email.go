package output

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"html"
	"image"
	"image/jpeg"
	"image/png"
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
	"golang.org/x/image/draw"
	"golang.org/x/net/proxy"
	"gopkg.in/gomail.v2"
)

var briefingMarkdownPattern = regexp.MustCompile(`^(\d{2}\.\d{2}\.\d{2})-(凌晨|早间|午间|晚间)-(\d{4})\.md$`)
var htmlURLPattern = regexp.MustCompile(`https?://[A-Za-z0-9._~:/?#\[\]@!$&'()*+,;=%-]+`)

const maxEmailInlineImageDimension = 1600
const maxEmailInlineImageBytes = 1 * 1024 * 1024
const maxEmailInlineImagePixels = 50_000_000
const emailInlineImageResizePercent = 85
const emailInlineJPEGQuality = 82

type smtpSendFunc func(*config.Config, string, string, string) error
type smtpHTMLSendFunc func(*config.Config, string, string, string, []emailInlineImage, string) error
type directEmailDialContextFactory func(time.Duration) func(context.Context, string, string) (net.Conn, error)

type emailInlineImage struct {
	Path string
	CID  string
}
type socks5EmailDialContextFactory func(string, time.Duration) (func(context.Context, string, string) (net.Conn, error), error)

type EmailSender struct {
	smtpSend                  smtpSendFunc
	smtpHTMLSend              smtpHTMLSendFunc
	newDirectEmailDialContext directEmailDialContextFactory
	newSocks5EmailDialContext socks5EmailDialContextFactory
	sleep                     func(time.Duration)
}

func emailRecipients(cfg *config.Config) []string {
	recipients := make([]string, 0, 1+len(cfg.Email.Recipients))
	if recipient := strings.TrimSpace(cfg.Email.To); recipient != "" {
		recipients = append(recipients, recipient)
	}
	for _, recipient := range cfg.Email.Recipients {
		if recipient = strings.TrimSpace(recipient); recipient != "" {
			recipients = append(recipients, recipient)
		}
	}
	return recipients
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

func SendAlertEmail(subject string, body string, cfg *config.Config) error {
	return NewEmailSender().SendAlertEmail(subject, body, cfg)
}

func (s *EmailSender) SendAlertEmail(subject string, body string, cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if strings.TrimSpace(subject) == "" {
		return fmt.Errorf("alert email subject must not be empty")
	}
	return s.sendHTMLEmailWithContent(cfg, subject, body)
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
	body := string(data)
	resolver := newEmailInlineImageResolver(filepath.Dir(absPath), outputDir, realOutputDir)
	defer resolver.cleanup()
	htmlBody := buildHTMLBodyWithImageResolver(body, resolver.resolve)
	return s.sendHTMLEmailWithRenderedContent(cfg, subject, body, htmlBody, resolver.images)
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
	return s.sendHTMLEmailWithRenderedContent(cfg, subject, body, buildHTMLBody(body), nil)
}

func (s *EmailSender) sendHTMLEmailWithRenderedContent(cfg *config.Config, subject string, body string, htmlBody string, inlineImages []emailInlineImage) error {
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

	var lastErr error
	for attempt := 1; attempt <= cfg.Email.RetryTimes; attempt++ {
		if err := send(cfg, subject, body, htmlBody, inlineImages, password); err != nil {
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
	m.SetHeader("To", emailRecipients(cfg)...)
	m.SetHeader("Subject", subject)
	m.SetBody("text/plain", body)
	return s.deliverMessage(cfg, m, password)
}

func (s *EmailSender) deliverSMTPHTMLMessage(cfg *config.Config, subject string, textBody string, htmlBody string, inlineImages []emailInlineImage, password string) error {
	return s.deliverMessage(cfg, newHTMLMessage(cfg, subject, textBody, htmlBody, inlineImages), password)
}

func newHTMLMessage(cfg *config.Config, subject string, textBody string, htmlBody string, inlineImages []emailInlineImage) *gomail.Message {
	m := gomail.NewMessage()
	m.SetHeader("From", cfg.Email.From)
	m.SetHeader("To", emailRecipients(cfg)...)
	m.SetHeader("Subject", subject)
	m.SetBody("text/plain", textBody)
	m.AddAlternative("text/html", htmlBody)
	for _, image := range inlineImages {
		m.Embed(image.Path, gomail.SetHeader(map[string][]string{"Content-ID": {"<" + image.CID + ">"}}))
	}
	return m
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
	for _, recipient := range emailRecipients(cfg) {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("send email: %w", err)
		}
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

type emailInlineImageResolver struct {
	markdownDir   string
	outputDir     string
	realOutputDir string
	byPath        map[string]emailInlineImage
	images        []emailInlineImage
	tempDir       string
}

func newEmailInlineImageResolver(markdownDir string, outputDir string, realOutputDir string) *emailInlineImageResolver {
	return &emailInlineImageResolver{
		markdownDir:   markdownDir,
		outputDir:     outputDir,
		realOutputDir: realOutputDir,
		byPath:        map[string]emailInlineImage{},
	}
}

func (r *emailInlineImageResolver) resolve(raw string) string {
	raw = strings.TrimSpace(html.UnescapeString(raw))
	path, ok := r.resolvePath(raw)
	if !ok && isRemoteMarkdownImage(raw) {
		path, ok = r.downloadRemote(raw)
	}
	if !ok {
		return raw
	}
	if image, ok := r.byPath[path]; ok {
		return "cid:" + image.CID
	}
	embedPath, err := r.prepare(path)
	if err != nil {
		return ""
	}
	image := emailInlineImage{Path: embedPath, CID: fmt.Sprintf("news-briefing-image-%d@news-briefing", len(r.images)+1)}
	r.byPath[path] = image
	r.images = append(r.images, image)
	return "cid:" + image.CID
}

func (r *emailInlineImageResolver) resolvePath(raw string) (string, bool) {
	if raw == "" || isRemoteMarkdownImage(raw) || strings.HasPrefix(raw, "data:") || strings.HasPrefix(raw, "cid:") {
		return "", false
	}
	path := raw
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.markdownDir, filepath.FromSlash(path))
	}
	path = filepath.Clean(path)
	if !pathWithinDir(path, r.outputDir) {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	if !pathWithinDir(realPath, r.realOutputDir) {
		return "", false
	}
	return path, true
}

func (r *emailInlineImageResolver) downloadRemote(raw string) (string, bool) {
	tempDir, err := r.ensureTempDir()
	if err != nil {
		return "", false
	}
	path := filepath.Join(tempDir, markdownImageFileName(raw))
	if err := downloadMarkdownImage(raw, path); err != nil {
		return "", false
	}
	return path, true
}

func (r *emailInlineImageResolver) prepare(path string) (string, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil {
		return "", err
	}
	if !emailInlineImageDimensionsAreSafe(config.Width, config.Height) {
		return "", fmt.Errorf("email inline image dimensions %dx%d exceed %d pixels", config.Width, config.Height, maxEmailInlineImagePixels)
	}
	if config.Width <= maxEmailInlineImageDimension && config.Height <= maxEmailInlineImageDimension && len(source) <= maxEmailInlineImageBytes {
		return path, nil
	}

	decoded, decodedFormat, err := image.Decode(bytes.NewReader(source))
	if err != nil {
		return "", err
	}
	if decodedFormat != "" {
		format = decodedFormat
	}
	width, height := fitEmailInlineImageDimensions(decoded.Bounds().Dx(), decoded.Bounds().Dy())
	candidate := decoded
	if width != decoded.Bounds().Dx() || height != decoded.Bounds().Dy() {
		candidate = resizeEmailInlineImage(decoded, width, height)
	}

	var encoded []byte
	var extension string
	for {
		encoded, extension, err = encodeEmailInlineImage(candidate, format)
		if err != nil {
			return "", err
		}
		if len(encoded) <= maxEmailInlineImageBytes {
			break
		}
		width, height, ok := nextEmailInlineImageSize(candidate.Bounds())
		if !ok {
			return "", fmt.Errorf("compress email inline image below %d bytes", maxEmailInlineImageBytes)
		}
		candidate = resizeEmailInlineImage(candidate, width, height)
	}

	tempDir, err := r.ensureTempDir()
	if err != nil {
		return "", err
	}
	file, err := os.CreateTemp(tempDir, "inline-*"+extension)
	if err != nil {
		return "", err
	}
	preparedPath := file.Name()
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		_ = os.Remove(preparedPath)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(preparedPath)
		return "", err
	}
	return preparedPath, nil
}

func emailInlineImageDimensionsAreSafe(width int, height int) bool {
	return width > 0 && height > 0 && height <= maxEmailInlineImagePixels && width <= maxEmailInlineImagePixels/height
}

func (r *emailInlineImageResolver) ensureTempDir() (string, error) {
	if r.tempDir != "" {
		return r.tempDir, nil
	}
	dir, err := os.MkdirTemp("", "news-briefing-email-images-*")
	if err != nil {
		return "", err
	}
	r.tempDir = dir
	return dir, nil
}

func fitEmailInlineImageDimensions(width int, height int) (int, int) {
	if width <= maxEmailInlineImageDimension && height <= maxEmailInlineImageDimension {
		return width, height
	}
	if width >= height {
		return maxEmailInlineImageDimension, max(1, height*maxEmailInlineImageDimension/width)
	}
	return max(1, width*maxEmailInlineImageDimension/height), maxEmailInlineImageDimension
}

func nextEmailInlineImageSize(bounds image.Rectangle) (int, int, bool) {
	width := bounds.Dx()
	height := bounds.Dy()
	nextWidth := max(1, width*emailInlineImageResizePercent/100)
	nextHeight := max(1, height*emailInlineImageResizePercent/100)
	if nextWidth == width && nextHeight == height {
		return width, height, false
	}
	return nextWidth, nextHeight, true
}

func resizeEmailInlineImage(source image.Image, width int, height int) image.Image {
	resized := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(resized, resized.Bounds(), source, source.Bounds(), draw.Over, nil)
	return resized
}

func encodeEmailInlineImage(source image.Image, format string) ([]byte, string, error) {
	var encoded bytes.Buffer
	switch format {
	case "jpeg":
		if err := jpeg.Encode(&encoded, source, &jpeg.Options{Quality: emailInlineJPEGQuality}); err != nil {
			return nil, "", err
		}
		return encoded.Bytes(), ".jpg", nil
	case "png":
		if err := png.Encode(&encoded, source); err != nil {
			return nil, "", err
		}
		return encoded.Bytes(), ".png", nil
	default:
		return nil, "", fmt.Errorf("unsupported email inline image format %q", format)
	}
}

func (r *emailInlineImageResolver) cleanup() {
	if r.tempDir != "" {
		_ = os.RemoveAll(r.tempDir)
	}
}

func pathWithinDir(path string, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func buildHTMLBody(body string) string {
	return buildHTMLBodyWithImageResolver(body, nil)
}

func buildHTMLBodyWithImageResolver(body string, resolveImageURL func(string) string) string {
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
.briefing-image{margin:12px 0 16px;}.briefing-image img{display:block;width:100%;height:auto;border-radius:14px;border:1px solid #eef2f7;}.hero-image{margin:-4px 0 24px;}
.briefing-section{margin:28px 0 0;padding:0;}.briefing-section:first-of-type{margin-top:0}.briefing-section h2{margin:0 0 14px;padding-top:18px;border-top:1px solid #edf0f5;font-size:17px;color:#1f2937;}.briefing-section:first-of-type h2{padding-top:0;border-top:0}.briefing-section h2 span{margin-left:8px;font-size:13px;font-weight:500;color:#6b7280;}
.section-news h2{padding-top:0;border-top:0;color:#2563eb;border-left:4px solid #60a5fa;padding-left:10px;}
.section-overview{padding:16px 18px;border:1px solid #dbeafe;border-radius:14px;background:#eff6ff;}.section-overview h2{padding-top:0;border-top:0;color:#1d4ed8;}.overview-category{margin:12px 0 6px;font-size:15px;color:#1f2937;}.overview-item{margin:0 0 7px;color:#374151;}
.section-status,.section-follow{padding:16px 18px;border:1px solid #eef2f7;border-radius:14px;background:#f8fafc;}.section-status h2,.section-follow h2{padding-top:0;border-top:0;color:#0f766e;}.section-candidates{padding:0;border:0;background:transparent;}.section-candidates h2{padding-top:0;border-top:0;color:#7c3aed;border-left:4px solid #a78bfa;padding-left:10px;}
.warning-block{padding:16px 18px;border:1px solid #fed7aa;border-radius:14px;background:#fff7ed;}.warning-block h2{margin:0 0 10px;padding-top:0;border-top:0;color:#c2410c;}.warning-item{margin:8px 0;color:#7c2d12;}
.news-item{padding:14px 0 18px;border-bottom:1px solid #f0f2f6;}.news-item:last-child{border-bottom:0;padding-bottom:0;}
h3{margin:0 0 8px;font-size:16px;line-height:1.45;color:#111827;}.summary,.impact,.markdown-label{margin:0 0 10px;color:#374151;}.summary strong,.impact strong,.markdown-label strong{color:#111827;}.meta{margin:0 0 10px;color:#6b7280;font-size:13px;}.source-link{display:inline-block;color:#2563eb;text-decoration:none;font-size:13px;font-weight:600;}code{font-family:SFMono-Regular,Consolas,"Liberation Mono",Menlo,monospace;font-size:12px;background:#f3f4f6;border:1px solid #e5e7eb;border-radius:6px;padding:1px 5px;color:#111827;}
.paragraph{margin:0 0 12px;color:#374151;}
@media(max-width:640px){.newsletter-shell{padding:0}.newsletter-card{border-radius:0;border-left:0;border-right:0;padding:20px 18px}h1{font-size:21px}}
</style>
</head>
<body><main class="newsletter-shell"><section class="newsletter-card">` + renderNewsletterHTMLWithImageResolver(body, resolveImageURL) + `</section></main></body>
</html>`
}

func renderNewsletterHTML(body string) string {
	return renderNewsletterHTMLWithImageResolver(body, nil)
}

func renderNewsletterHTMLWithImageResolver(body string, resolveImageURL func(string) string) string {
	renderer := newsletterHTMLRenderer{resolveImageURL: resolveImageURL}
	return renderer.render(body)
}

type newsletterHTMLRenderer struct {
	out               strings.Builder
	resolveImageURL   func(string) string
	inArticle         bool
	inSection         bool
	inWarningSection  bool
	inOverviewSection bool
}

func (r *newsletterHTMLRenderer) render(body string) string {
	lines := strings.Split(body, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || line == "---" {
			continue
		}
		if r.renderDocumentTitle(line, i) {
			continue
		}
		if r.renderMarkdownImage(line) {
			continue
		}
		if r.renderCategoryLine(line) {
			continue
		}
		if r.renderFetchWarningHeader(line, lines, i) {
			continue
		}
		if r.renderWarningItem(line) {
			continue
		}
		if r.renderOverviewItem(line) {
			continue
		}
		if r.renderArticleTitle(line) {
			continue
		}
		if r.renderSummary(line) {
			continue
		}
		if r.renderImpact(line) {
			continue
		}
		if r.renderStrongLabel(line) {
			continue
		}
		if r.renderSourceMeta(line) {
			continue
		}
		if r.renderSourceLink(line) {
			continue
		}
		r.renderParagraph(line)
	}
	r.closeSection()
	return r.out.String()
}

func (r *newsletterHTMLRenderer) closeArticle() {
	if r.inArticle {
		r.out.WriteString("</article>")
		r.inArticle = false
	}
}

func (r *newsletterHTMLRenderer) closeSection() {
	r.closeArticle()
	if r.inSection {
		r.out.WriteString("</section>")
		r.inSection = false
		r.inWarningSection = false
		r.inOverviewSection = false
	}
}

func (r *newsletterHTMLRenderer) openSection(title, count string) {
	r.closeSection()
	sectionClass := htmlSectionClass(title)
	r.inWarningSection = strings.Contains(sectionClass, "warning-block")
	r.inOverviewSection = sectionClass == "section-overview"
	r.out.WriteString(`<section class="briefing-section `)
	r.out.WriteString(sectionClass)
	r.out.WriteString(`"><h2>`)
	r.out.WriteString(html.EscapeString(title))
	if count != "" {
		r.out.WriteString(" <span>")
		r.out.WriteString(html.EscapeString(count))
		r.out.WriteString("</span>")
	}
	r.out.WriteString("</h2>")
	r.inSection = true
}

func (r *newsletterHTMLRenderer) renderDocumentTitle(line string, index int) bool {
	if title, ok := strings.CutPrefix(line, "# "); ok {
		r.closeSection()
		r.out.WriteString("<h1>")
		r.out.WriteString(html.EscapeString(strings.TrimSpace(title)))
		r.out.WriteString("</h1>")
		return true
	}
	if index == 0 {
		r.closeSection()
		r.out.WriteString("<h1>")
		r.out.WriteString(html.EscapeString(line))
		r.out.WriteString("</h1>")
		return true
	}
	return false
}

func (r *newsletterHTMLRenderer) renderMarkdownImage(line string) bool {
	alt, imageURL, ok := parseMarkdownImage(line)
	if !ok {
		return false
	}
	if r.resolveImageURL != nil {
		imageURL = r.resolveImageURL(imageURL)
	}
	r.out.WriteString(renderMarkdownImageHTML(alt, imageURL))
	return true
}

func (r *newsletterHTMLRenderer) renderCategoryLine(line string) bool {
	category, count, ok := parseHTMLCategoryLine(line)
	if !ok {
		return false
	}
	r.openSection(category, count)
	return true
}

func (r *newsletterHTMLRenderer) renderFetchWarningHeader(line string, lines []string, index int) bool {
	if line != "抓取异常" {
		return false
	}
	if !hasRenderableHTMLWarningItems(lines, index+1) {
		return true
	}
	r.openSection(line, "")
	return true
}

func (r *newsletterHTMLRenderer) renderWarningItem(line string) bool {
	if !r.inWarningSection {
		return false
	}
	item, ok := strings.CutPrefix(line, "- ")
	if !ok {
		return false
	}
	r.out.WriteString(`<p class="warning-item">`)
	r.out.WriteString(linkifyHTML(cleanMarkdownLine(item), "链接"))
	r.out.WriteString("</p>")
	return true
}

func (r *newsletterHTMLRenderer) renderOverviewItem(line string) bool {
	if !r.inOverviewSection {
		return false
	}
	if title, ok := strings.CutPrefix(line, "### "); ok {
		r.closeArticle()
		r.out.WriteString(`<h3 class="overview-category">`)
		r.out.WriteString(html.EscapeString(strings.TrimSpace(title)))
		r.out.WriteString("</h3>")
		return true
	}
	if item, ok := strings.CutPrefix(line, "- "); ok {
		r.out.WriteString(`<p class="overview-item">`)
		r.out.WriteString(linkifyHTML(cleanMarkdownLine(item), "链接"))
		r.out.WriteString("</p>")
		return true
	}
	return false
}

func (r *newsletterHTMLRenderer) renderArticleTitle(line string) bool {
	title, ok := parseHTMLArticleTitle(line)
	if !ok {
		return false
	}
	r.closeArticle()
	r.out.WriteString(`<article class="news-item"><h3>`)
	r.out.WriteString(html.EscapeString(title))
	r.out.WriteString("</h3>")
	r.inArticle = true
	return true
}

func (r *newsletterHTMLRenderer) renderSummary(line string) bool {
	summary, ok := strings.CutPrefix(line, "**摘要：**")
	if !ok {
		return false
	}
	r.out.WriteString(`<p class="summary"><strong>摘要：</strong>`)
	r.out.WriteString(linkifyHTML(cleanMarkdownLine(summary), "链接"))
	r.out.WriteString("</p>")
	return true
}

func (r *newsletterHTMLRenderer) renderImpact(line string) bool {
	impact, ok := strings.CutPrefix(line, "**影响：**")
	if !ok {
		return false
	}
	r.out.WriteString(`<p class="impact"><strong>影响：</strong>`)
	r.out.WriteString(linkifyHTML(cleanMarkdownLine(impact), "链接"))
	r.out.WriteString("</p>")
	return true
}

func (r *newsletterHTMLRenderer) renderStrongLabel(line string) bool {
	label, content, ok := parseHTMLStrongLabelLine(line)
	if !ok {
		return false
	}
	r.out.WriteString(`<p class="markdown-label"><strong>`)
	r.out.WriteString(html.EscapeString(label))
	r.out.WriteString(`</strong>`)
	r.out.WriteString(renderInlineMarkdownHTML(cleanMarkdownLine(content), "链接"))
	r.out.WriteString("</p>")
	return true
}

func (r *newsletterHTMLRenderer) renderSourceMeta(line string) bool {
	if source, ok := strings.CutPrefix(line, "> 来源: "); ok {
		r.writeSourceMeta(source)
		return true
	}
	if source, ok := strings.CutPrefix(line, "Source: "); ok {
		r.writeSourceMeta(source)
		return true
	}
	return false
}

func (r *newsletterHTMLRenderer) writeSourceMeta(source string) {
	r.out.WriteString(`<p class="meta">`)
	r.out.WriteString(html.EscapeString(strings.ReplaceAll(cleanMarkdownLine(source), " | ", " · ")))
	r.out.WriteString("</p>")
}

func (r *newsletterHTMLRenderer) renderSourceLink(line string) bool {
	rawURL, ok := strings.CutPrefix(line, "Link: ")
	if !ok {
		return false
	}
	r.out.WriteString(`<a class="source-link" href="`)
	r.out.WriteString(html.EscapeString(strings.TrimSpace(rawURL)))
	r.out.WriteString(`">原文链接</a>`)
	return true
}

func (r *newsletterHTMLRenderer) renderParagraph(line string) {
	r.out.WriteString(`<p class="paragraph">`)
	r.out.WriteString(linkifyHTML(line, "链接"))
	r.out.WriteString("</p>")
}

func parseMarkdownImage(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "![") || !strings.HasSuffix(line, ")") {
		return "", "", false
	}
	closeAlt := strings.Index(line, "](")
	if closeAlt < 2 {
		return "", "", false
	}
	alt := strings.TrimSpace(line[2:closeAlt])
	imageURL := strings.TrimSpace(line[closeAlt+2 : len(line)-1])
	if imageURL == "" {
		return "", "", false
	}
	return alt, imageURL, true
}

func renderMarkdownImageHTML(alt string, imageURL string) string {
	imageURL = html.UnescapeString(imageURL)
	if strings.TrimSpace(imageURL) == "" {
		return ""
	}
	className := "briefing-image"
	if strings.TrimSpace(alt) == "封面图" {
		className += " hero-image"
	}
	return `<figure class="` + className + `"><img src="` + html.EscapeString(imageURL) + `" alt="` + html.EscapeString(alt) + `"></figure>`
}

func htmlSectionClass(title string) string {
	switch strings.TrimSpace(title) {
	case "今日速览":
		return "section-overview"
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
		if _, ok := strings.CutPrefix(line, "- "); !ok {
			return false
		}
		return true
	}
	return false
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
	return AppendFailedSection(briefingHeaderBlock(briefing)+briefing.RawContent, failed)
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

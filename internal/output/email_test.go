package output

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/model"
)

func TestBuildEmailBodyPreservesBodyOrderAndSingleTitle(t *testing.T) {
	briefing := &model.Briefing{
		Date:       "26.03.27",
		Period:     "1400",
		RawContent: "TRANSLATED\n\n---\n\nORIGINAL",
	}

	got := buildEmailBody(briefing, nil)
	title := "国际资讯简报 26.03.27 午间 14:00"
	if strings.Count(got, title) != 1 {
		t.Fatalf("buildEmailBody() title count = %d, want 1 in %q", strings.Count(got, title), got)
	}
	if !strings.Contains(got, "TRANSLATED\n\n---\n\nORIGINAL") {
		t.Fatalf("buildEmailBody() body = %q", got)
	}
}

func TestBuildEmailBodyOmitsFailedSectionWhenNoFailures(t *testing.T) {
	briefing := &model.Briefing{
		Date:       "26.03.18",
		Period:     "1400",
		RawContent: "## AI/科技\n\n正文",
	}

	got := buildEmailBody(briefing, nil)
	if strings.Contains(got, "抓取异常") {
		t.Fatalf("buildEmailBody() = %q, want no failure section", got)
	}
}

func TestBuildEmailBodyAppendsFailedSection(t *testing.T) {
	briefing := &model.Briefing{
		Date:       "26.03.18",
		Period:     "1400",
		RawContent: "## AI/科技\n\n正文",
	}
	failed := []fetcher.FailedSource{{
		Name: "Reddit Singularity",
		Err:  errors.New("http error: 403 Forbidden"),
	}}

	got := buildEmailBody(briefing, failed)
	wantParts := []string{
		"国际资讯简报 26.03.18 午间 14:00",
		"## AI/科技",
		"---\n抓取异常",
		"- Reddit Singularity: http error: 403 Forbidden",
	}
	for _, want := range wantParts {
		if !strings.Contains(got, want) {
			t.Fatalf("buildEmailBody() = %q, want substring %q", got, want)
		}
	}
}

func TestBuildDeepEmailBodyUsesDeepTitle(t *testing.T) {
	briefing := &model.Briefing{RawContent: "正文"}
	got := buildDeepEmailBody("Claude", briefing, nil)
	if !strings.Contains(got, "国际资讯话题深挖 | Claude") {
		t.Fatalf("buildDeepEmailBody() = %q", got)
	}
}

func TestBuildDeepEmailBodyAppendsFailedSection(t *testing.T) {
	briefing := &model.Briefing{RawContent: "正文"}
	failed := []fetcher.FailedSource{{
		Name: "HN",
		Err:  errors.New("timeout"),
	}}

	got := buildDeepEmailBody("Claude", briefing, failed)
	wantParts := []string{
		"国际资讯话题深挖 | Claude",
		"正文",
		"---\n抓取异常",
		"- HN: timeout",
	}
	for _, want := range wantParts {
		if !strings.Contains(got, want) {
			t.Fatalf("buildDeepEmailBody() = %q, want substring %q", got, want)
		}
	}
}

func TestBuildHTMLBodyRendersStructuredNewsletter(t *testing.T) {
	body := strings.Join([]string{
		"国际资讯简报 26.05.26 晚间 18:00",
		"",
		"== AI/科技 (1篇) ==",
		"",
		"1. Claude Code 更新 <Beta>",
		"**摘要：** 这是摘要内容。",
		"   Source: Anthropic | 2026-05-26 18:00",
		"   Link: https://example.com/articles/very-long-path?utm_source=rss&from=mail",
		"",
	}, "\n")

	got := buildHTMLBody(body)
	wantParts := []string{
		"newsletter-shell",
		"newsletter-card",
		"<h1>国际资讯简报 26.05.26 晚间 18:00</h1>",
		"<h2>AI/科技 <span>1篇</span></h2>",
		`<article class="news-item">`,
		"<h3>Claude Code 更新 &lt;Beta&gt;</h3>",
		`<p class="summary"><strong>摘要：</strong>这是摘要内容。</p>`,
		`<p class="meta">Anthropic · 2026-05-26 18:00</p>`,
		`<a class="source-link" href="https://example.com/articles/very-long-path?utm_source=rss&amp;from=mail">原文链接</a>`,
		"overflow-wrap:anywhere",
	}
	for _, want := range wantParts {
		if !strings.Contains(got, want) {
			t.Fatalf("buildHTMLBody() = %q, want substring %q", got, want)
		}
	}
	if strings.Contains(got, "email-body") || strings.Contains(got, "white-space:pre-wrap") {
		t.Fatalf("buildHTMLBody() still renders as pre-wrapped text dump: %q", got)
	}
	if strings.Contains(got, ">https://example.com/articles/very-long-path") {
		t.Fatalf("buildHTMLBody() exposes long URL as visible text: %q", got)
	}
}

func TestBuildHTMLBodyRendersBriefingMarkdownHeadings(t *testing.T) {
	body := strings.Join([]string{
		"# 国际资讯简报 26.05.26 晚间 18:00",
		"",
		"## AI/科技",
		"",
		"### 优步称AI支出越来越难证明合理",
		"**摘要：** 优步总裁表示，公司越来越难证明高额AI投入的合理性。  ",
		"**影响：** 大型企业可能转向更严格的ROI审查。  ",
		"> 来源: The Verge；Reddit Singularity | 2026-05-26 17:55；17:44",
		"",
	}, "\n")

	got := buildHTMLBody(body)
	wantParts := []string{
		"<h1>国际资讯简报 26.05.26 晚间 18:00</h1>",
		"<h2>AI/科技</h2>",
		`<article class="news-item">`,
		"<h3>优步称AI支出越来越难证明合理</h3>",
		`<p class="summary"><strong>摘要：</strong>优步总裁表示，公司越来越难证明高额AI投入的合理性。</p>`,
		`<p class="impact"><strong>影响：</strong>大型企业可能转向更严格的ROI审查。</p>`,
		`<p class="meta">The Verge；Reddit Singularity · 2026-05-26 17:55；17:44</p>`,
	}
	for _, want := range wantParts {
		if !strings.Contains(got, want) {
			t.Fatalf("buildHTMLBody() = %q, want substring %q", got, want)
		}
	}
	if strings.Contains(got, "# 国际资讯") || strings.Contains(got, "**摘要：**") || strings.Contains(got, "&gt; 来源") {
		t.Fatalf("buildHTMLBody() leaked markdown syntax: %q", got)
	}
}

func TestBuildHTMLBodyStylesBriefingSectionsAndWarningBlocks(t *testing.T) {
	body := strings.Join([]string{
		"# 国际资讯简报 26.05.26 晚间 18:00",
		"",
		"## AI/科技",
		"AI 正文",
		"",
		"---",
		"## 今日态势",
		"中东仍是今日最大风险源。",
		"",
		"---",
		"## 未命中关键词的候选新闻",
		"候选新闻列表。",
		"",
		"## Watch 站点异常",
		"- Watch 站点异常：Claude Platform Release Notes — 抓取失败：release notes fragment \"plugins\" not found",
		"",
		"---",
		"抓取异常",
		"- X coverage/OpenAI: target may not fully cover requested window: limit-reached",
	}, "\n")

	got := renderNewsletterHTML(body)
	wantParts := []string{
		`<section class="briefing-section section-news"><h2>AI/科技</h2>`,
		`<section class="briefing-section section-status"><h2>今日态势</h2>`,
		`<section class="briefing-section section-candidates"><h2>未命中关键词的候选新闻</h2>`,
		`<section class="briefing-section warning-block watch-warning"><h2>Watch 站点异常</h2>`,
		`<p class="warning-item">Watch 站点异常：Claude Platform Release Notes — 抓取失败：release notes fragment &#34;plugins&#34; not found</p>`,
		`<section class="briefing-section warning-block fetch-warning"><h2>抓取异常</h2>`,
		`<p class="warning-item">X coverage/OpenAI: target may not fully cover requested window: limit-reached</p>`,
	}
	for _, want := range wantParts {
		if !strings.Contains(got, want) {
			t.Fatalf("renderNewsletterHTML() = %q, want substring %q", got, want)
		}
	}
	if strings.Contains(got, `<p class="paragraph">---</p>`) {
		t.Fatalf("renderNewsletterHTML() rendered markdown separator: %q", got)
	}

	withoutWarnings := renderNewsletterHTML(strings.Join([]string{
		"# 国际资讯简报 26.05.26 晚间 18:00",
		"",
		"## AI/科技",
		"AI 正文",
	}, "\n"))
	if strings.Contains(withoutWarnings, "warning-block") || strings.Contains(withoutWarnings, "warning-item") {
		t.Fatalf("renderNewsletterHTML() rendered warning styling without warning sections: %q", withoutWarnings)
	}
}

func TestBuildHTMLBodyStylesCategoryTitlesWithoutCandidateBackground(t *testing.T) {
	body := strings.Join([]string{
		"# 国际资讯简报 26.05.26 晚间 18:00",
		"",
		"## AI/科技",
		"AI 正文",
		"",
		"## 国际政治",
		"政治正文",
		"",
		"## 未命中关键词的候选新闻",
		"候选新闻列表。",
	}, "\n")

	got := buildHTMLBody(body)
	wantParts := []string{
		`.section-news h2{padding-top:0;border-top:0;color:#2563eb;`,
		`.section-candidates{padding:0;border:0;background:transparent;`,
		`.section-candidates h2{padding-top:0;border-top:0;color:#7c3aed;`,
		`<section class="briefing-section section-news"><h2>AI/科技</h2>`,
		`<section class="briefing-section section-news"><h2>国际政治</h2>`,
		`<section class="briefing-section section-candidates"><h2>未命中关键词的候选新闻</h2>`,
	}
	for _, want := range wantParts {
		if !strings.Contains(got, want) {
			t.Fatalf("buildHTMLBody() = %q, want substring %q", got, want)
		}
	}
	if strings.Contains(got, `.section-candidates{padding:16px 18px`) || strings.Contains(got, `.section-candidates{background:#`) {
		t.Fatalf("buildHTMLBody() gives candidate section a card background: %q", got)
	}
}

func TestBuildHTMLBodySuppressesStaleSearchLimitReachedWarningItems(t *testing.T) {
	body := strings.Join([]string{
		"# 国际资讯简报 26.05.26 晚间 18:00",
		"",
		"抓取异常",
		"- X coverage/search:Codex limits: target may not fully cover requested window: limit-reached",
		"- X coverage/OpenAI: target may not fully cover requested window: limit-reached",
	}, "\n")

	got := renderNewsletterHTML(body)
	if strings.Contains(got, "X coverage/search:Codex") {
		t.Fatalf("renderNewsletterHTML() kept stale search limit-reached warning: %q", got)
	}
	if !strings.Contains(got, "X coverage/OpenAI") {
		t.Fatalf("renderNewsletterHTML() removed account limit-reached warning too: %q", got)
	}
}

func TestBuildHTMLBodyOmitsFetchWarningBlockWhenOnlySearchLimitReachedWasSuppressed(t *testing.T) {
	body := strings.Join([]string{
		"# 国际资讯简报 26.05.26 晚间 18:00",
		"",
		"抓取异常",
		"- X coverage/search:Codex limits: target may not fully cover requested window: limit-reached",
	}, "\n")

	got := renderNewsletterHTML(body)
	if strings.Contains(got, "fetch-warning") || strings.Contains(got, "抓取异常") || strings.Contains(got, "X coverage/search:Codex") {
		t.Fatalf("renderNewsletterHTML() kept an empty fetch warning block: %q", got)
	}
}

func TestBuildHTMLBodyRendersFollowDirectionBoldLabels(t *testing.T) {
	body := strings.Join([]string{
		"# 国际资讯简报 26.05.26 晚间 18:00",
		"",
		"## 今日最值得追的方向",
		"",
		"### 方向1：美国对伊朗打击与霍尔木兹风险",
		"**为什么值得追：** 这是原因。  ",
		"**接下来关注什么：** 看会议纪要。",
		"**深挖命令：** `./news-briefing deep \"US Iran\" --ignore-seen`",
	}, "\n")

	got := renderNewsletterHTML(body)
	wantParts := []string{
		`<p class="markdown-label"><strong>为什么值得追：</strong>这是原因。</p>`,
		`<p class="markdown-label"><strong>接下来关注什么：</strong>看会议纪要。</p>`,
		`<p class="markdown-label"><strong>深挖命令：</strong><code>./news-briefing deep &#34;US Iran&#34; --ignore-seen</code></p>`,
	}
	for _, want := range wantParts {
		if !strings.Contains(got, want) {
			t.Fatalf("renderNewsletterHTML() = %q, want substring %q", got, want)
		}
	}
	if strings.Contains(got, "**为什么值得追：**") || strings.Contains(got, "`./news-briefing") {
		t.Fatalf("renderNewsletterHTML() leaked markdown emphasis syntax: %q", got)
	}
}

func TestSendMarkdownFileUsesHTMLMailPathWithPlainFallback(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "output")
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	path := filepath.Join(outputDir, "26.04.13-晚间-1800.md")
	body := "简报\n   Link: https://example.com/articles/very-long-path?utm_source=rss&from=mail"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := &config.Config{Output: config.OutputCfg{Dir: outputDir}, Email: config.Email{RetryTimes: 1}}
	sender := NewEmailSender()
	sender.smtpSend = func(cfg *config.Config, subject, body, password string) error {
		return errors.New("plain text SMTP sender should not be used for email delivery")
	}
	var gotSubject string
	var gotText string
	var gotHTML string
	sender.smtpHTMLSend = func(cfg *config.Config, subject, textBody, htmlBody, password string) error {
		gotSubject = subject
		gotText = textBody
		gotHTML = htmlBody
		return nil
	}
	t.Setenv("EMAIL_SMTP_AUTH_CODE", "secret")

	if err := sender.SendMarkdownFile(path, cfg); err != nil {
		t.Fatalf("SendMarkdownFile() error = %v", err)
	}
	if gotSubject != "国际资讯简报 26.04.13 晚间 18:00" {
		t.Fatalf("subject = %q", gotSubject)
	}
	if gotText != body {
		t.Fatalf("plain fallback = %q, want %q", gotText, body)
	}
	if !strings.Contains(gotHTML, `<a class="source-link" href="https://example.com/articles/very-long-path?utm_source=rss&amp;from=mail">原文链接</a>`) {
		t.Fatalf("html body = %q", gotHTML)
	}
	if strings.Contains(gotHTML, ">https://example.com/articles/very-long-path") {
		t.Fatalf("html body exposes long URL as visible text: %q", gotHTML)
	}
}

func TestEmailSenderRejectsNilInputs(t *testing.T) {
	sender := NewEmailSender()
	cfg := &config.Config{}
	briefing := &model.Briefing{}
	tests := []struct {
		name    string
		send    func() error
		wantErr string
	}{
		{
			name:    "send email nil config",
			send:    func() error { return sender.SendEmail(briefing, nil, nil) },
			wantErr: "config is nil",
		},
		{
			name:    "send email nil briefing",
			send:    func() error { return sender.SendEmail(nil, cfg, nil) },
			wantErr: "briefing is nil",
		},
		{
			name:    "send deep email nil config",
			send:    func() error { return sender.SendDeepEmail("Claude", briefing, nil, nil) },
			wantErr: "config is nil",
		},
		{
			name:    "send deep email nil briefing",
			send:    func() error { return sender.SendDeepEmail("Claude", nil, cfg, nil) },
			wantErr: "briefing is nil",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.send()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("send() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestBriefingSubjectFromMarkdownFilename(t *testing.T) {
	subject, err := briefingSubjectFromMarkdownFilename("output/26.04.13-晚间-1800.md")
	if err != nil {
		t.Fatalf("briefingSubjectFromMarkdownFilename() error = %v", err)
	}
	if subject != "国际资讯简报 26.04.13 晚间 18:00" {
		t.Fatalf("subject = %q", subject)
	}
}

func TestBriefingSubjectFromMarkdownFilenameRejectsInvalidName(t *testing.T) {
	_, err := briefingSubjectFromMarkdownFilename("output/bad-name.md")
	if err == nil || !strings.Contains(err.Error(), "markdown filename") {
		t.Fatalf("briefingSubjectFromMarkdownFilename() error = %v", err)
	}
}

func TestSendMarkdownFileRejectsFileOutsideOutputDir(t *testing.T) {
	cfg := &config.Config{Output: config.OutputCfg{Dir: "output"}}
	err := SendMarkdownFile("/tmp/26.04.13-晚间-1800.md", cfg)
	if err == nil || !strings.Contains(err.Error(), "outside output dir") {
		t.Fatalf("SendMarkdownFile() error = %v", err)
	}
}

func TestSendMarkdownFileRejectsSymlinkEscapingOutputDir(t *testing.T) {
	dir := t.TempDir()
	outsidePath := filepath.Join(dir, "secret.md")
	if err := os.WriteFile(outsidePath, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	outputDir := filepath.Join(dir, "output")
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	linkPath := filepath.Join(outputDir, "26.04.13-晚间-1800.md")
	if err := os.Symlink(outsidePath, linkPath); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	cfg := &config.Config{Output: config.OutputCfg{Dir: outputDir}}
	err := SendMarkdownFile(linkPath, cfg)
	if err == nil || !strings.Contains(err.Error(), "outside output dir") {
		t.Fatalf("SendMarkdownFile() error = %v", err)
	}
}

func TestSendMarkdownFileRejectsSymlinkPathOutsideOutputDirEvenWhenTargetInside(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "output")
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	realPath := filepath.Join(outputDir, "26.04.13-晚间-1800.md")
	if err := os.WriteFile(realPath, []byte("real body"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	linkDir := filepath.Join(dir, "links")
	if err := os.Mkdir(linkDir, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	linkPath := filepath.Join(linkDir, "26.04.13-晚间-1800.md")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	cfg := &config.Config{Output: config.OutputCfg{Dir: outputDir}, Email: config.Email{RetryTimes: 1}}
	sender := NewEmailSender()
	sender.smtpSend = func(cfg *config.Config, subject, body, password string) error { return nil }
	if err := os.Setenv("EMAIL_SMTP_AUTH_CODE", "secret"); err != nil {
		t.Fatalf("Setenv() error = %v", err)
	}
	defer os.Unsetenv("EMAIL_SMTP_AUTH_CODE")

	err := sender.SendMarkdownFile(linkPath, cfg)
	if err == nil || !strings.Contains(err.Error(), "outside output dir") {
		t.Fatalf("SendMarkdownFile() error = %v", err)
	}
}

func TestValidateEmailReadyForSending(t *testing.T) {
	t.Setenv("EMAIL_SMTP_AUTH_CODE", "secret")
	cfg := &config.Config{Email: config.Email{SMTPHost: "smtp.example.com", SMTPPort: 465, From: "from@example.com", To: "to@example.com"}}

	if err := ValidateEmailReadyForSending(cfg); err != nil {
		t.Fatalf("ValidateEmailReadyForSending() error = %v", err)
	}
}

func TestValidateEmailReadyForSendingRejectsMissingAuthCode(t *testing.T) {
	t.Setenv("EMAIL_SMTP_AUTH_CODE", "")
	cfg := &config.Config{Email: config.Email{SMTPHost: "smtp.example.com", SMTPPort: 465, From: "from@example.com", To: "to@example.com"}}

	err := ValidateEmailReadyForSending(cfg)
	if err == nil || !strings.Contains(err.Error(), "EMAIL_SMTP_AUTH_CODE") {
		t.Fatalf("ValidateEmailReadyForSending() error = %v, want SMTP auth code error", err)
	}
}

func TestValidateEmailReadyForSendingRejectsMissingSocks5ProxyWhenEnabled(t *testing.T) {
	t.Setenv("EMAIL_SMTP_AUTH_CODE", "secret")
	cfg := &config.Config{Email: config.Email{SMTPHost: "smtp.example.com", SMTPPort: 465, From: "from@example.com", To: "to@example.com", UseProxy: true}}

	err := ValidateEmailReadyForSending(cfg)
	if err == nil || !strings.Contains(err.Error(), "proxy.socks5") {
		t.Fatalf("ValidateEmailReadyForSending() error = %v, want proxy.socks5 error", err)
	}
}

func TestEmailDialContextUsesConfiguredSocks5Proxy(t *testing.T) {
	capturedAddr := ""
	capturedTimeout := time.Duration(0)
	sender := NewEmailSender()
	sender.newSocks5EmailDialContext = func(proxyAddr string, timeout time.Duration) (func(context.Context, string, string) (net.Conn, error), error) {
		capturedAddr = proxyAddr
		capturedTimeout = timeout
		return func(ctx context.Context, network, address string) (net.Conn, error) {
			return nil, errors.New("stop")
		}, nil
	}

	cfg := &config.Config{
		Email: config.Email{Timeout: 2 * time.Second, UseProxy: true},
		Proxy: config.Proxy{Socks5: "socks5://127.0.0.1:1080"},
	}
	_, err := sender.newEmailDialContext(cfg)
	if err != nil {
		t.Fatalf("newEmailDialContext() error = %v", err)
	}
	if capturedAddr != "socks5://127.0.0.1:1080" {
		t.Fatalf("proxy addr = %q, want socks5://127.0.0.1:1080", capturedAddr)
	}
	if capturedTimeout != 2*time.Second {
		t.Fatalf("timeout = %v, want %v", capturedTimeout, 2*time.Second)
	}
}

func TestEmailDialContextRejectsMissingSocks5ProxyWhenEnabled(t *testing.T) {
	cfg := &config.Config{Email: config.Email{Timeout: time.Second, UseProxy: true}}
	_, err := newEmailDialContext(cfg)
	if err == nil || !strings.Contains(err.Error(), "proxy.socks5") {
		t.Fatalf("newEmailDialContext() error = %v", err)
	}
}

func TestEmailDialerDirectIgnoresProxyEnvWhenDisabled(t *testing.T) {
	capturedTimeout := time.Duration(0)
	sender := NewEmailSender()
	sender.newDirectEmailDialContext = func(timeout time.Duration) func(ctx context.Context, network, address string) (net.Conn, error) {
		capturedTimeout = timeout
		return func(ctx context.Context, network, address string) (net.Conn, error) {
			return nil, errors.New("stop")
		}
	}

	cfg := &config.Config{Email: config.Email{SMTPHost: "smtp.example.com", SMTPPort: 465, Timeout: time.Second, UseProxy: false}}
	err := sender.deliverSMTPMessage(cfg, "subject", "body", "secret")
	if err == nil || !strings.Contains(err.Error(), "stop") {
		t.Fatalf("deliverSMTPMessage() error = %v", err)
	}
	if capturedTimeout != time.Second {
		t.Fatalf("capturedTimeout = %v, want %v", capturedTimeout, time.Second)
	}
}

func TestSendEmailWithRetryStopsAfterSuccess(t *testing.T) {
	cfg := &config.Config{Email: config.Email{RetryTimes: 3, RetryWaitTime: 0, UseProxy: false}}
	attempts := 0
	sender := NewEmailSender()
	sender.smtpSend = func(cfg *config.Config, subject, body, password string) error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary timeout")
		}
		return nil
	}
	sender.sleep = func(time.Duration) {}
	if err := os.Setenv("EMAIL_SMTP_AUTH_CODE", "secret"); err != nil {
		t.Fatalf("Setenv() error = %v", err)
	}
	defer os.Unsetenv("EMAIL_SMTP_AUTH_CODE")

	if err := sender.sendEmailWithContent(cfg, "subject", "body"); err != nil {
		t.Fatalf("sendEmailWithContent() error = %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestSendEmailWithRetryReturnsLastError(t *testing.T) {
	cfg := &config.Config{Email: config.Email{RetryTimes: 3, RetryWaitTime: 0, UseProxy: false}}
	sender := NewEmailSender()
	sender.smtpSend = func(cfg *config.Config, subject, body, password string) error {
		return errors.New("temporary timeout")
	}
	sender.sleep = func(time.Duration) {}
	if err := os.Setenv("EMAIL_SMTP_AUTH_CODE", "secret"); err != nil {
		t.Fatalf("Setenv() error = %v", err)
	}
	defer os.Unsetenv("EMAIL_SMTP_AUTH_CODE")

	err := sender.sendEmailWithContent(cfg, "subject", "body")
	if err == nil || !strings.Contains(err.Error(), "after 3 attempts") {
		t.Fatalf("sendEmailWithContent() error = %v", err)
	}
}

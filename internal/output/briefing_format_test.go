package output

import "testing"

func TestBriefingFormatTitle(t *testing.T) {
	if got := briefingTitle("26.03.22", "0800"); got != "国际资讯简报 26.03.22 早间 08:00" {
		t.Fatalf("briefingTitle() = %q", got)
	}
}

func TestBriefingFormatMarkdownHeader(t *testing.T) {
	if got := briefingMarkdownHeader("26.03.22", "0800"); got != "# 国际资讯简报 26.03.22 早间 08:00" {
		t.Fatalf("briefingMarkdownHeader() = %q", got)
	}
}

func TestBriefingFormatEmailSubject(t *testing.T) {
	if got := briefingEmailSubject("26.03.22", "0800"); got != "[资讯简报] 26.03.22 早间 08:00" {
		t.Fatalf("briefingEmailSubject() = %q", got)
	}
}

func TestBriefingFormatFileName(t *testing.T) {
	if got := briefingFileName("26.03.22", "0800"); got != "26.03.22-早间-0800.md" {
		t.Fatalf("briefingFileName() = %q", got)
	}
}

func TestBriefingFormatHandlesInvalidPeriodWithoutPanic(t *testing.T) {
	if got := briefingTitle("26.03.22", "800"); got != "国际资讯简报 26.03.22 800 800" {
		t.Fatalf("briefingTitle() = %q", got)
	}
}

func TestDeepEmailSubject(t *testing.T) {
	if got := deepEmailSubject("Claude"); got != "[资讯简报] 话题深挖 | Claude" {
		t.Fatalf("deepEmailSubject() = %q", got)
	}
}

func TestDeepEmailTitle(t *testing.T) {
	if got := deepEmailTitle("Claude"); got != "国际资讯话题深挖 | Claude" {
		t.Fatalf("deepEmailTitle() = %q", got)
	}
}

package output

import (
	"testing"

	"github.com/walker1211/news-briefing/internal/model"
)

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

func TestBriefingHeaderBlockUsesDefaultTitleWithoutHeroImage(t *testing.T) {
	briefing := &model.Briefing{
		Date:       "26.06.06",
		Period:     "0800",
		Articles:   []model.Article{{ImageURL: "https://example.com/hero.jpg"}},
		RawContent: "## AI/科技\n\n### Claude 服务异常\n**摘要：** ...",
	}
	want := "# 国际资讯简报 26.06.06 早间 08:00\n\n"
	if got := briefingHeaderBlock(briefing); got != want {
		t.Fatalf("briefingHeaderBlock() = %q, want %q", got, want)
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

func TestBriefingFormatIndexFileName(t *testing.T) {
	if got := briefingIndexFileName("26.03.22", "0800"); got != "26.03.22-早间-0800.json" {
		t.Fatalf("briefingIndexFileName() = %q", got)
	}
}

func TestWatchFileName(t *testing.T) {
	if got := watchFileName("26.04.15", "1600"); got != "26.04.15-午间-1600-watch.md" {
		t.Fatalf("watchFileName() = %q", got)
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

package output

import (
	"errors"
	"strings"
	"testing"

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

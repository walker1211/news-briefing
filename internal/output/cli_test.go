package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/walker1211/news-briefing/internal/model"
)

func TestWriteCLIPreservesBodyOrderAndSingleTitle(t *testing.T) {
	var buf bytes.Buffer
	writeCLI(&buf, &model.Briefing{
		Date:       "26.03.27",
		Period:     "1400",
		RawContent: "TRANSLATED\n\n---\n\nORIGINAL",
	}, false)

	got := buf.String()
	title := "国际资讯简报 26.03.27 午间 14:00"
	if strings.Count(got, title) != 1 {
		t.Fatalf("writeCLI() title count = %d, want 1 in %q", strings.Count(got, title), got)
	}
	if !strings.Contains(got, "TRANSLATED\n\n---\n\nORIGINAL") {
		t.Fatalf("writeCLI() body = %q", got)
	}
}

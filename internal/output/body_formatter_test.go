package output

import (
	"strings"
	"testing"

	"github.com/walker1211/news-briefing/internal/model"
)

func TestFormatBodyOriginalOnly(t *testing.T) {
	got, err := FormatBody("news-briefing run", model.OutputModeOriginalOnly, model.OutputContent{
		Title:      "国际资讯简报 26.03.27 早间 08:00",
		Original:   "Original body",
		Translated: "Translated body",
	})
	if err != nil {
		t.Fatalf("FormatBody() error = %v", err)
	}
	if got != "Original body" {
		t.Fatalf("FormatBody() = %q, want %q", got, "Original body")
	}
}

func TestFormatBodyTranslatedOnly(t *testing.T) {
	got, err := FormatBody("news-briefing run", model.OutputModeTranslatedOnly, model.OutputContent{
		Original:   "Original body",
		Translated: "Translated body",
	})
	if err != nil {
		t.Fatalf("FormatBody() error = %v", err)
	}
	if got != "Translated body" {
		t.Fatalf("FormatBody() = %q, want %q", got, "Translated body")
	}
}

func TestFormatBodyBilingualTranslatedFirst(t *testing.T) {
	got, err := FormatBody("news-briefing run", model.OutputModeBilingualTranslatedFirst, model.OutputContent{
		Original:   "Original body",
		Translated: "Translated body",
	})
	if err != nil {
		t.Fatalf("FormatBody() error = %v", err)
	}

	want := "Translated body\n\n---\n\nOriginal body"
	if got != want {
		t.Fatalf("FormatBody() = %q, want %q", got, want)
	}
}

func TestFormatBodyBilingualOriginalFirst(t *testing.T) {
	got, err := FormatBody("news-briefing run", model.OutputModeBilingualOriginalFirst, model.OutputContent{
		Original:   "Original body",
		Translated: "Translated body",
	})
	if err != nil {
		t.Fatalf("FormatBody() error = %v", err)
	}

	want := "Original body\n\n---\n\nTranslated body"
	if got != want {
		t.Fatalf("FormatBody() = %q, want %q", got, want)
	}
}

func TestFormatBodyRejectsMissingRequiredSide(t *testing.T) {
	testCases := []struct {
		name        string
		mode        model.OutputMode
		original    string
		translated  string
		missingSide string
	}{
		{
			name:        "original_only requires original",
			mode:        model.OutputModeOriginalOnly,
			translated:  "Translated body",
			missingSide: "original",
		},
		{
			name:        "translated_only requires translated",
			mode:        model.OutputModeTranslatedOnly,
			original:    "Original body",
			missingSide: "translated",
		},
		{
			name:        "bilingual translated first requires original",
			mode:        model.OutputModeBilingualTranslatedFirst,
			translated:  "Translated body",
			missingSide: "original",
		},
		{
			name:        "bilingual translated first rejects whitespace-only translated",
			mode:        model.OutputModeBilingualTranslatedFirst,
			original:    "Original body",
			translated:  " \n\t ",
			missingSide: "translated",
		},
		{
			name:        "bilingual original first rejects whitespace-only original",
			mode:        model.OutputModeBilingualOriginalFirst,
			original:    "\n\t ",
			translated:  "Translated body",
			missingSide: "original",
		},
		{
			name:        "bilingual original first requires translated",
			mode:        model.OutputModeBilingualOriginalFirst,
			original:    "Original body",
			missingSide: "translated",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := FormatBody("cmds/news-briefing run", tc.mode, model.OutputContent{
				Original:   tc.original,
				Translated: tc.translated,
			})
			if err == nil {
				t.Fatal("FormatBody() error = nil, want error")
			}
			if !strings.Contains(err.Error(), "cmds/news-briefing run") {
				t.Fatalf("FormatBody() error = %q, want mention command path", err)
			}
			if !strings.Contains(err.Error(), string(tc.mode)) {
				t.Fatalf("FormatBody() error = %q, want mention mode", err)
			}
			if !strings.Contains(err.Error(), tc.missingSide) {
				t.Fatalf("FormatBody() error = %q, want mention missing side", err)
			}
		})
	}
}

func TestFormatBodyDoesNotRenderTitle(t *testing.T) {
	got, err := FormatBody("news-briefing run", model.OutputModeTranslatedOnly, model.OutputContent{
		Translated: "Translated body",
		Title:      "国际资讯简报 26.03.27 早间 08:00",
	})
	if err != nil {
		t.Fatalf("FormatBody() error = %v", err)
	}
	if strings.Contains(got, "国际资讯简报") {
		t.Fatalf("FormatBody() = %q, should not include title", got)
	}
}

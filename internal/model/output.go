package model

import "fmt"

type OutputMode string

const (
	OutputModeOriginalOnly             OutputMode = "original_only"
	OutputModeTranslatedOnly           OutputMode = "translated_only"
	OutputModeBilingualTranslatedFirst OutputMode = "bilingual_translated_first"
	OutputModeBilingualOriginalFirst   OutputMode = "bilingual_original_first"
)

type OutputContent struct {
	Title      string
	Original   string
	Translated string
}

type BriefingSummary struct {
	OverviewGroups []BriefingOverviewGroup `json:"overview_groups"`
	Stories        []BriefingStory         `json:"stories"`
	Situation      string                  `json:"situation"`
	Directions     []BriefingDirection     `json:"directions"`
	XHSTopics      []string                `json:"xhs_topics,omitempty"`
}

type BriefingOverviewGroup struct {
	Category string   `json:"category"`
	Items    []string `json:"items"`
}

type BriefingStory struct {
	Category          string `json:"category"`
	ContentType       string `json:"content_type,omitempty"`
	EvidenceLevel     string `json:"evidence_level,omitempty"`
	Title             string `json:"title"`
	ImageURL          string `json:"image_url,omitempty"`
	Summary           string `json:"summary"`
	Impact            string `json:"impact"`
	SourceArticleIDs  []int  `json:"source_article_ids,omitempty"`
	SourceLine        string `json:"source_line,omitempty"`
	CarryoverRequired bool   `json:"carryover_required,omitempty"`
}

type BriefingDirection struct {
	Title       string `json:"title"`
	Why         string `json:"why"`
	Next        string `json:"next"`
	DeepCommand string `json:"deep_command"`
}

func (m OutputMode) Valid() bool {
	switch m {
	case OutputModeOriginalOnly,
		OutputModeTranslatedOnly,
		OutputModeBilingualTranslatedFirst,
		OutputModeBilingualOriginalFirst:
		return true
	default:
		return false
	}
}

func (m OutputMode) Validate() error {
	if m.Valid() {
		return nil
	}
	return fmt.Errorf("invalid output mode %q", m)
}

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

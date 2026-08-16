package model

import "strings"

const (
	SourceRolePrimary  = "primary"
	SourceRoleOriginal = "original"
	SourceRoleRadar    = "radar"
	SourceRoleRepost   = "repost"

	ContentTypeNews    = "news"
	ContentTypeTool    = "tool"
	ContentTypeCase    = "case"
	ContentTypeInsight = "insight"

	EvidenceCorroborated = "corroborated"
	EvidenceSupported    = "supported"
	EvidenceSingleSource = "single_source"
)

func ValidSourceRole(value string) bool {
	switch strings.TrimSpace(value) {
	case SourceRolePrimary, SourceRoleOriginal, SourceRoleRadar, SourceRoleRepost:
		return true
	default:
		return false
	}
}

func ValidContentType(value string) bool {
	switch strings.TrimSpace(value) {
	case ContentTypeNews, ContentTypeTool, ContentTypeCase, ContentTypeInsight:
		return true
	default:
		return false
	}
}

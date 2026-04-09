package model

import "time"

type Article struct {
	Title     string    `json:"title"`
	Link      string    `json:"link"`
	Summary   string    `json:"summary"`
	Source    string    `json:"source"`
	Category  string    `json:"category"`
	Published time.Time `json:"published"`
}

type TraceStatus string

const (
	TraceStatusIncluded         TraceStatus = "included"
	TraceStatusKeywordMiss      TraceStatus = "keyword_miss"
	TraceStatusOutOfWindow      TraceStatus = "out_of_window"
	TraceStatusDuplicateInBatch TraceStatus = "duplicate_in_batch"
	TraceStatusSeenBefore       TraceStatus = "seen_before"
	TraceStatusFetchFailed      TraceStatus = "fetch_failed"
)

type ArticleTrace struct {
	Title           string      `json:"title"`
	Link            string      `json:"link"`
	Source          string      `json:"source"`
	SourceType      string      `json:"source_type"`
	Category        string      `json:"category"`
	Published       time.Time   `json:"published"`
	MatchedKeywords []string    `json:"matched_keywords"`
	Status          TraceStatus `json:"status"`
	RejectionReason string      `json:"rejection_reason"`
}

type SourceRun struct {
	Name             string `json:"name"`
	Type             string `json:"type"`
	Category         string `json:"category"`
	Status           string `json:"status"`
	Error            string `json:"error,omitempty"`
	FetchedCount     int    `json:"fetched_count"`
	KeywordMissCount int    `json:"keyword_miss_count"`
	WindowMissCount  int    `json:"window_miss_count"`
	DedupedCount     int    `json:"deduped_count"`
	IncludedCount    int    `json:"included_count"`
}

type SourceIndex struct {
	SourceRuns    []SourceRun    `json:"source_runs"`
	ArticleTraces []ArticleTrace `json:"article_traces"`
}

type Briefing struct {
	Date       string
	Period     string // "HHMM" 格式，如 "0800"、"1400"、"2000"
	Articles   []Article
	Summary    string // Claude 生成的摘要
	RawContent string // 完整的 Markdown 内容
}

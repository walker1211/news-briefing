package model

import "time"

type SourceStatsReport struct {
	SchemaVersion string             `json:"schema_version"`
	SourceApp     string             `json:"source_app"`
	Date          string             `json:"date,omitempty"`
	Period        string             `json:"period,omitempty"`
	GeneratedAt   time.Time          `json:"generated_at"`
	Window        SourceStatsWindow  `json:"window"`
	Totals        SourceStatsTotals  `json:"totals"`
	Sources       []SourceStatsEntry `json:"sources"`
	Failed        []SourceStatsError `json:"failed,omitempty"`
}

type SourceStatsWindow struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type SourceStatsTotals struct {
	Fetched             int   `json:"fetched"`
	InWindow            int   `json:"in_window"`
	KeywordMatched      int   `json:"keyword_matched"`
	Filtered            int   `json:"filtered"`
	FilteredKeywordMiss int   `json:"filtered_keyword_miss"`
	FilteredExcluded    int   `json:"filtered_excluded"`
	FilteredSourceLimit int   `json:"filtered_source_limit"`
	AcceptedBeforeDedup int   `json:"accepted_before_dedup"`
	AcceptedAfterDedup  int   `json:"accepted_after_dedup"`
	EnteredAI           int   `json:"entered_ai"`
	SelectedFinal       int   `json:"selected_final"`
	FetchDurationMS     int64 `json:"fetch_duration_ms"`
	ResponseBytes       int64 `json:"response_bytes"`
}

type SourceStatsEntry struct {
	Source              string `json:"source"`
	Type                string `json:"type,omitempty"`
	Category            string `json:"category,omitempty"`
	Fetched             int    `json:"fetched"`
	InWindow            int    `json:"in_window"`
	KeywordMatched      int    `json:"keyword_matched"`
	Filtered            int    `json:"filtered"`
	FilteredKeywordMiss int    `json:"filtered_keyword_miss"`
	FilteredExcluded    int    `json:"filtered_excluded"`
	FilteredSourceLimit int    `json:"filtered_source_limit"`
	AcceptedBeforeDedup int    `json:"accepted_before_dedup"`
	AcceptedAfterDedup  int    `json:"accepted_after_dedup"`
	EnteredAI           int    `json:"entered_ai"`
	SelectedFinal       int    `json:"selected_final"`
	FetchDurationMS     int64  `json:"fetch_duration_ms,omitempty"`
	ResponseBytes       int64  `json:"response_bytes,omitempty"`
	CacheStatus         string `json:"cache_status,omitempty"`
}

type SourceStatsError struct {
	Source string `json:"source"`
	Error  string `json:"error"`
}

func (report *SourceStatsReport) SetEnteredAI(articles []Article) {
	if report == nil {
		return
	}
	for i := range report.Sources {
		report.Sources[i].EnteredAI = 0
	}
	indexBySource := make(map[string]int, len(report.Sources))
	for i, source := range report.Sources {
		indexBySource[source.Source] = i
	}
	for _, article := range articles {
		source := article.Source
		if source == "" {
			source = "unknown"
		}
		index, ok := indexBySource[source]
		if !ok {
			report.Sources = append(report.Sources, SourceStatsEntry{
				Source:   source,
				Type:     "watch",
				Category: article.Category,
			})
			index = len(report.Sources) - 1
			indexBySource[source] = index
		}
		report.Sources[index].EnteredAI++
		if report.Sources[index].Category == "" {
			report.Sources[index].Category = article.Category
		}
	}
	report.RecalculateTotals()
}

func (report *SourceStatsReport) SetSelectedFinal(articles []Article) {
	if report == nil {
		return
	}
	for i := range report.Sources {
		report.Sources[i].SelectedFinal = 0
	}
	indexBySource := make(map[string]int, len(report.Sources))
	for i, source := range report.Sources {
		indexBySource[source.Source] = i
	}
	for _, article := range articles {
		source := article.Source
		if source == "" {
			source = "unknown"
		}
		index, ok := indexBySource[source]
		if !ok {
			report.Sources = append(report.Sources, SourceStatsEntry{
				Source:   source,
				Type:     "watch",
				Category: article.Category,
			})
			index = len(report.Sources) - 1
			indexBySource[source] = index
		}
		report.Sources[index].SelectedFinal++
		if report.Sources[index].Category == "" {
			report.Sources[index].Category = article.Category
		}
	}
	report.RecalculateTotals()
}

func (report *SourceStatsReport) RecalculateTotals() {
	if report == nil {
		return
	}
	var totals SourceStatsTotals
	for _, source := range report.Sources {
		totals.Fetched += source.Fetched
		totals.InWindow += source.InWindow
		totals.KeywordMatched += source.KeywordMatched
		totals.Filtered += source.Filtered
		totals.FilteredKeywordMiss += source.FilteredKeywordMiss
		totals.FilteredExcluded += source.FilteredExcluded
		totals.FilteredSourceLimit += source.FilteredSourceLimit
		totals.AcceptedBeforeDedup += source.AcceptedBeforeDedup
		totals.AcceptedAfterDedup += source.AcceptedAfterDedup
		totals.EnteredAI += source.EnteredAI
		totals.SelectedFinal += source.SelectedFinal
		totals.FetchDurationMS += source.FetchDurationMS
		totals.ResponseBytes += source.ResponseBytes
	}
	report.Totals = totals
}

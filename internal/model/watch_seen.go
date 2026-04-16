package model

import "time"

type WatchSeenArticle struct {
	ID               string    `json:"id"`
	URL              string    `json:"url"`
	Title            string    `json:"title"`
	Source           string    `json:"source"`
	BriefingCategory string    `json:"briefing_category"`
	WatchCategory    string    `json:"watch_category"`
	Summary          string    `json:"summary"`
	Body             string    `json:"body"`
	EventType        string    `json:"event_type"`
	DetectedAt       time.Time `json:"detected_at"`
}

type WatchSeenState struct {
	Items []WatchSeenArticle `json:"items"`
}

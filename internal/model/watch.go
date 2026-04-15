package model

import "time"

type WatchIndexSnapshot struct {
	Scope      string           `json:"scope"`
	Source     string           `json:"source"`
	Category   string           `json:"category,omitempty"`
	URL        string           `json:"url"`
	SnapshotAt time.Time        `json:"snapshot_at"`
	ItemCount  int              `json:"item_count"`
	Items      []WatchIndexItem `json:"items"`
	Hash       string           `json:"hash"`
}

type WatchIndexItem struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Position    int    `json:"position"`
	Snippet     string `json:"snippet,omitempty"`
	UpdatedText string `json:"updated_text,omitempty"`
	ItemHash    string `json:"item_hash"`
}

type WatchArticleState struct {
	URL           string    `json:"url"`
	Title         string    `json:"title"`
	SummaryHash   string    `json:"summary_hash"`
	BodyHash      string    `json:"body_hash"`
	LastCheckedAt time.Time `json:"last_checked_at"`
	LastChangedAt time.Time `json:"last_changed_at"`
}

type WatchEvent struct {
	EventType         string    `json:"event_type"`
	Source            string    `json:"source"`
	Category          string    `json:"category"`
	ArticleURL        string    `json:"article_url,omitempty"`
	ArticleTitle      string    `json:"article_title,omitempty"`
	DetectedAt        time.Time `json:"detected_at"`
	MatchedKeywords   []string  `json:"matched_keywords,omitempty"`
	IncludeInBriefing bool      `json:"include_in_briefing"`
	Reason            string    `json:"reason"`
	BodyFetched       bool      `json:"body_fetched"`
	ContentChanged    bool      `json:"content_changed"`
}

type WatchReport struct {
	GeneratedAt time.Time    `json:"generated_at"`
	Events      []WatchEvent `json:"events"`
}

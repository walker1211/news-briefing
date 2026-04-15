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

type Briefing struct {
	Date       string
	Period     string // "HHMM" 格式，如 "0800"、"1400"、"2000"
	Articles   []Article
	Summary    string // Claude 生成的摘要
	RawContent string // 完整的 Markdown 内容
}

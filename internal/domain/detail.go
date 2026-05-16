package domain

import "time"

// ItemDetail contains optional source-native detail content for a feed item.
type ItemDetail struct {
	ItemID    string
	Title     string
	URL       string
	LoadedAt  time.Time
	Sections  []DetailSection
	Comments  []DetailComment
	Providers []SourceID
}

// DetailSection is a named Markdown-capable detail block.
type DetailSection struct {
	Title  string
	Body   string
	URL    string
	Source SourceID
}

// DetailComment is a normalized top-level source discussion comment.
type DetailComment struct {
	Author      string
	Body        string
	URL         string
	Score       *int64
	PublishedAt *time.Time
	Depth       int
	Source      SourceID
}

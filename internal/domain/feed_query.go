package domain

// FeedQuery controls visible feed reads across application and store layers.
type FeedQuery struct {
	Source        SourceID
	IncludeHidden bool
	Search        string
	SourceView    string
	SavedOnly     bool
	UnreadOnly    bool
	Language      string
	Tag           string
	Limit         int
}

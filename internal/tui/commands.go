package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"

	"github.com/AIluffy/tildewire/internal/app"
	"github.com/AIluffy/tildewire/internal/domain"
)

const (
	refreshProgressInterval = 200 * time.Millisecond
	refreshProgressTimeout  = 2 * time.Second
	toastDuration           = 1400 * time.Millisecond
)

var writeClipboard = clipboard.WriteAll

func batchCommands(cmds ...tea.Cmd) tea.Cmd {
	nonNil := make([]tea.Cmd, 0, len(cmds))
	for _, cmd := range cmds {
		if cmd != nil {
			nonNil = append(nonNil, cmd)
		}
	}
	switch len(nonNil) {
	case 0:
		return nil
	case 1:
		return nonNil[0]
	default:
		return tea.Batch(nonNil...)
	}
}

func (m Model) loadCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		snapshot, err := m.service.LoadFeed(ctx, m.view, m.filter)
		return snapshotMsg{snapshot: snapshot, err: err, message: "feed loaded"}
	}
}

func (m Model) loadWithMessageCmd(message string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		snapshot, err := m.service.LoadFeed(ctx, m.view, m.filter)
		return snapshotMsg{snapshot: snapshot, err: err, message: message}
	}
}

func (m Model) searchLoadCmd(searchLoadID int, message string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		snapshot, err := m.service.LoadFeed(ctx, m.view, m.filter)
		return snapshotMsg{searchLoadID: searchLoadID, snapshot: snapshot, err: err, message: message}
	}
}

func (m Model) loadThenRefreshCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		snapshot, err := m.service.LoadFeed(ctx, m.view, m.filter)
		return snapshotMsg{
			snapshot:    snapshot,
			err:         err,
			message:     "feed loaded",
			nextRefresh: &refreshRequest{mode: app.RefreshModeVisible},
		}
	}
}

func (m Model) refreshWithProgressCmd(refreshID int, force bool, mode app.RefreshMode) tea.Cmd {
	return tea.Batch(m.refreshCmd(refreshID, force, mode), m.refreshProgressCmd(refreshID), m.loadingSpinner.Tick)
}

func (m Model) refreshCmd(refreshID int, force bool, mode app.RefreshMode) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		snapshot, err := m.service.Refresh(ctx, m.view, m.filter, app.RefreshOptions{Force: force, Mode: mode})
		return snapshotMsg{refreshID: refreshID, snapshot: snapshot, err: err, message: "refresh complete"}
	}
}

func (m Model) refreshProgressCmd(refreshID int) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(refreshProgressInterval)
		ctx, cancel := context.WithTimeout(context.Background(), refreshProgressTimeout)
		defer cancel()
		snapshot, err := m.service.LoadFeed(ctx, m.view, m.filter)
		return refreshProgressMsg{refreshID: refreshID, snapshot: snapshot, err: err}
	}
}

func (m Model) setSavedCmd(itemID string, saved bool) tea.Cmd {
	message := "item unsaved"
	if saved {
		message = "item saved"
	}
	if m.canPatchSavedState() {
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := m.service.SetSaved(ctx, itemID, saved)
			return itemStatePatchMsg{itemID: itemID, saved: &saved, err: err, message: message}
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.service.SetSaved(ctx, itemID, saved); err != nil {
			return statusMsg{message: "item state failed", err: err}
		}
		snapshot, err := m.service.LoadFeed(ctx, m.view, m.filter)
		if err != nil {
			snapshot = m.currentSnapshot()
		}
		return snapshotMsg{snapshot: snapshot, err: err, message: message}
	}
}

func (m Model) setReadCmd(itemID string, read bool) tea.Cmd {
	message := "item marked unread"
	if read {
		message = "item marked read"
	}
	if m.canPatchReadState() {
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := m.service.SetRead(ctx, itemID, read)
			return itemStatePatchMsg{itemID: itemID, read: &read, err: err, message: message}
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.service.SetRead(ctx, itemID, read); err != nil {
			return statusMsg{message: "item state failed", err: err}
		}
		snapshot, err := m.service.LoadFeed(ctx, m.view, m.filter)
		if err != nil {
			snapshot = m.currentSnapshot()
		}
		return snapshotMsg{snapshot: snapshot, err: err, message: message}
	}
}

func (m Model) canPatchSavedState() bool {
	return m.view != "" && m.view != domain.SourceAll && m.view != domain.SourceRecommend && !m.filter.SavedOnly
}

func (m Model) canPatchReadState() bool {
	return m.view != "" && m.view != domain.SourceAll && m.view != domain.SourceRecommend && !m.filter.UnreadOnly
}

func (m Model) canPatchHiddenState() bool {
	return m.view != "" && m.view != domain.SourceAll && m.view != domain.SourceRecommend && m.filter.IncludeHidden
}

func (m Model) currentSnapshot() app.Snapshot {
	return app.Snapshot{
		Entries:          m.entries,
		Statuses:         m.statuses,
		FetchHistory:     m.fetchHistory,
		Rules:            m.rules,
		DedupeCandidates: m.dedupeCandidates,
		Counts:           m.counts,
		View:             m.view,
		Filter:           m.filter,
		LoadedAt:         time.Now().UTC(),
	}
}

func (m Model) setHiddenCmd(itemID string, hidden bool) tea.Cmd {
	message := "item restored"
	if hidden {
		message = "item hidden"
	}
	if m.canPatchHiddenState() {
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := m.service.SetHidden(ctx, itemID, hidden)
			return itemStatePatchMsg{itemID: itemID, hidden: &hidden, err: err, message: message}
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.service.SetHidden(ctx, itemID, hidden); err != nil {
			return statusMsg{message: "item state failed", err: err}
		}
		snapshot, err := m.service.LoadFeed(ctx, m.view, m.filter)
		if err != nil {
			snapshot = m.currentSnapshot()
		}
		return snapshotMsg{snapshot: snapshot, err: err, message: message}
	}
}

func (m Model) loadDetailCmd(entry domain.FeedEntry) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		detail, err := m.service.LoadDetail(ctx, entry)
		return detailMsg{itemID: entry.Item.ID, detail: detail, err: err}
	}
}

func (m Model) recommendDiagnosticsCmd(entry domain.FeedEntry) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		diagnostics, err := m.service.RecommendationDiagnostics(ctx, entry)
		return recommendDiagnosticsMsg{diagnostics: diagnostics, err: err}
	}
}

func (m Model) markdownImagePreviewCmd(itemID string, key markdownImagePreviewKey, request markdownImagePreviewRequest) tea.Cmd {
	previewer := m.imagePreviewer
	return func() tea.Msg {
		if previewer == nil {
			return markdownImagePreviewMsg{itemID: itemID, key: key, err: fmt.Errorf("markdown image previewer is not configured")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		result, err := previewer.RenderMarkdownImage(ctx, request)
		return markdownImagePreviewMsg{itemID: itemID, key: key, result: result, err: err}
	}
}

func (m Model) exportSavedCmd(format app.ExportFormat) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		dir := filepath.Join(m.exportBaseDir(), "exports")
		result, err := m.service.ExportSaved(ctx, app.ExportOptions{Format: format, Dir: dir})
		if err != nil {
			return statusMsg{message: "export failed", err: err}
		}
		return statusMsg{message: fmt.Sprintf("exported saved items: %s (%d)", result.Path, result.Count)}
	}
}

func (m Model) exportBaseDir() string {
	if strings.TrimSpace(m.config.DataDir) == "" {
		return "."
	}
	return m.config.DataDir
}

func (m Model) clearCacheCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		snapshot, err := m.service.ClearCache(ctx, m.view, m.filter)
		if err != nil {
			return statusMsg{message: "clear cache failed", err: err}
		}
		return snapshotMsg{
			snapshot:    snapshot,
			message:     "cache cleared",
			nextRefresh: &refreshRequest{force: true, mode: app.RefreshModeVisible},
		}
	}
}

func openURLCmd(url string) tea.Cmd {
	return func() tea.Msg {
		err := openExternalURL(url)
		return statusMsg{message: "opened url", err: err}
	}
}

func (m Model) openItemURLCmd(entry domain.FeedEntry) tea.Cmd {
	return m.openEntryURLCmd(entry, entry.Item.URL, domain.ItemEventOpenURL)
}

func (m Model) openSourceURLCmd(entry domain.FeedEntry) tea.Cmd {
	return m.openEntryURLCmd(entry, entry.Item.CommentsURL, domain.ItemEventOpenSource)
}

func (m Model) openEntryURLCmd(entry domain.FeedEntry, url string, eventType domain.ItemEventType) tea.Cmd {
	return func() tea.Msg {
		err := openExternalURL(url)
		if err == nil && strings.TrimSpace(url) != "" {
			m.recordItemEvent(entry, eventType)
		}
		return statusMsg{message: "opened url", err: err}
	}
}

func copyCmd(value, message string) tea.Cmd {
	return copyWithToastCmd(value, message, "")
}

func copyToastCmd(value, message string) tea.Cmd {
	return copyWithToastCmd(value, message, message)
}

func copyWithToastCmd(value, message, toast string) tea.Cmd {
	return func() tea.Msg {
		if strings.TrimSpace(value) == "" {
			return statusMsg{message: "nothing to copy", err: nil}
		}
		if err := writeClipboard(value); err != nil {
			return statusMsg{message: "copy failed", err: err}
		}
		return statusMsg{message: message, toast: toast}
	}
}

func (m Model) copyItemURLCmd(entry domain.FeedEntry) tea.Cmd {
	return m.copyEntryValueCmd(entry, entry.Item.URL, "copied url", "", domain.ItemEventCopyURL)
}

func (m Model) copyMarkdownLinkCmd(entry domain.FeedEntry) tea.Cmd {
	value := fmt.Sprintf("[%s](%s)", entry.Item.Title, entry.Item.URL)
	return m.copyEntryValueCmd(entry, value, "copied markdown link", "", domain.ItemEventCopyMarkdown)
}

func (m Model) copyEntryValueCmd(entry domain.FeedEntry, value, message, toast string, eventType domain.ItemEventType) tea.Cmd {
	return func() tea.Msg {
		if strings.TrimSpace(value) == "" {
			return statusMsg{message: "nothing to copy", err: nil}
		}
		if err := writeClipboard(value); err != nil {
			return statusMsg{message: "copy failed", err: err}
		}
		m.recordItemEvent(entry, eventType)
		return statusMsg{message: message, toast: toast}
	}
}

func (m Model) recordItemEvent(entry domain.FeedEntry, eventType domain.ItemEventType) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = m.service.RecordItemEvent(ctx, domain.ItemEvent{
		ItemID:    entry.Item.ID,
		EventType: eventType,
		Source:    entry.PrimarySource().Source,
		View:      m.view,
	})
}

func clearToastCmd(id int) tea.Cmd {
	return tea.Tick(toastDuration, func(time.Time) tea.Msg {
		return clearToastMsg(id)
	})
}

type snapshotMsg struct {
	refreshID    int
	searchLoadID int
	snapshot     app.Snapshot
	err          error
	message      string
	nextRefresh  *refreshRequest
}

type itemStatePatchMsg struct {
	itemID  string
	saved   *bool
	read    *bool
	hidden  *bool
	err     error
	message string
}

type refreshRequest struct {
	force bool
	mode  app.RefreshMode
}

type refreshProgressMsg struct {
	refreshID int
	snapshot  app.Snapshot
	err       error
}

type detailMsg struct {
	itemID string
	detail domain.ItemDetail
	err    error
}

type markdownImagePreviewMsg struct {
	itemID string
	key    markdownImagePreviewKey
	result markdownImagePreviewResult
	err    error
}

type recommendDiagnosticsMsg struct {
	diagnostics domain.RecommendationDiagnostics
	err         error
}

type statusMsg struct {
	message string
	toast   string
	err     error
}

type clearToastMsg int

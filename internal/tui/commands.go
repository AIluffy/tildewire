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
	defaultCommandTimeout   = 5 * time.Second
	exportCommandTimeout    = 10 * time.Second
	detailCommandTimeout    = 20 * time.Second
	refreshCommandTimeout   = 40 * time.Second
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

func timeoutCmd(timeout time.Duration, run func(context.Context) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return runWithTimeout(timeout, run)
	}
}

func runWithTimeout(timeout time.Duration, run func(context.Context) tea.Msg) tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return run(ctx)
}

func (m Model) loadCmd() tea.Cmd {
	return m.loadSnapshotCmd("feed loaded", nil)
}

func (m Model) loadWithMessageCmd(message string) tea.Cmd {
	return m.loadSnapshotCmd(message, nil)
}

func (m Model) searchLoadCmd(searchLoadID int, message string) tea.Cmd {
	return m.loadSnapshotCmd(message, func(msg *snapshotMsg) {
		msg.searchLoadID = searchLoadID
	})
}

func (m Model) loadThenRefreshCmd() tea.Cmd {
	return m.loadSnapshotCmd("feed loaded", func(msg *snapshotMsg) {
		msg.nextRefresh = &refreshRequest{mode: app.RefreshModeVisible}
	})
}

func (m Model) loadSnapshotCmd(message string, customize func(*snapshotMsg)) tea.Cmd {
	return timeoutCmd(defaultCommandTimeout, func(ctx context.Context) tea.Msg {
		snapshot, err := m.loadFeedSnapshot(ctx, m.view, m.filter)
		msg := snapshotMsg{snapshot: snapshot, err: err, message: message}
		if customize != nil {
			customize(&msg)
		}
		return msg
	})
}

func (m Model) refreshWithProgressCmd(refreshID int, force bool, mode app.RefreshMode) tea.Cmd {
	return tea.Batch(m.refreshCmd(refreshID, force, mode), m.refreshProgressCmd(refreshID), m.loadingSpinner.Tick)
}

func (m Model) refreshCmd(refreshID int, force bool, mode app.RefreshMode) tea.Cmd {
	return timeoutCmd(refreshCommandTimeout, func(ctx context.Context) tea.Msg {
		snapshot, err := m.service.Refresh(ctx, m.view, m.filter, app.RefreshOptions{Force: force, Mode: mode})
		return snapshotMsg{refreshID: refreshID, snapshot: snapshot, err: err, message: "refresh complete"}
	})
}

func (m Model) refreshProgressCmd(refreshID int) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(refreshProgressInterval)
		return runWithTimeout(refreshProgressTimeout, func(ctx context.Context) tea.Msg {
			snapshot, err := m.service.LoadFeed(ctx, m.view, m.filter)
			return refreshProgressMsg{refreshID: refreshID, snapshot: snapshot, err: err}
		})
	}
}

func (m Model) setSavedCmd(itemID string, saved bool) tea.Cmd {
	message := "item unsaved"
	if saved {
		message = "item saved"
	}
	return m.setItemStateCmd(
		itemID,
		message,
		m.canPatchSavedState,
		func(ctx context.Context, itemID string) error { return m.service.SetSaved(ctx, itemID, saved) },
		func(msg *itemStatePatchMsg) { msg.saved = &saved },
	)
}

func (m Model) setReadCmd(itemID string, read bool) tea.Cmd {
	message := "item marked unread"
	if read {
		message = "item marked read"
	}
	return m.setItemStateCmd(
		itemID,
		message,
		m.canPatchReadState,
		func(ctx context.Context, itemID string) error { return m.service.SetRead(ctx, itemID, read) },
		func(msg *itemStatePatchMsg) { msg.read = &read },
	)
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
	return m.setItemStateCmd(
		itemID,
		message,
		m.canPatchHiddenState,
		func(ctx context.Context, itemID string) error { return m.service.SetHidden(ctx, itemID, hidden) },
		func(msg *itemStatePatchMsg) { msg.hidden = &hidden },
	)
}

func (m Model) setItemStateCmd(itemID string, message string, canPatch func() bool, mutate func(context.Context, string) error, patch func(*itemStatePatchMsg)) tea.Cmd {
	if canPatch() {
		return timeoutCmd(defaultCommandTimeout, func(ctx context.Context) tea.Msg {
			msg := itemStatePatchMsg{itemID: itemID, err: mutate(ctx, itemID), message: message}
			patch(&msg)
			return msg
		})
	}
	return timeoutCmd(defaultCommandTimeout, func(ctx context.Context) tea.Msg {
		if err := mutate(ctx, itemID); err != nil {
			return statusMsg{message: "item state failed", err: err}
		}
		snapshot, err := m.loadFeedSnapshot(ctx, m.view, m.filter)
		if err != nil {
			snapshot = m.currentSnapshot()
		}
		return snapshotMsg{snapshot: snapshot, err: err, message: message}
	})
}

func (m Model) loadFeedSnapshot(ctx context.Context, view domain.SourceID, filter app.FeedFilter) (app.Snapshot, error) {
	if err := m.service.RefreshRecommendations(ctx); err != nil {
		return app.Snapshot{}, err
	}
	return m.service.LoadFeed(ctx, view, filter)
}

func (m Model) loadDetailCmd(entry domain.FeedEntry) tea.Cmd {
	requestID := m.detailRequestID
	return timeoutCmd(detailCommandTimeout, func(ctx context.Context) tea.Msg {
		detail, err := m.service.LoadDetail(ctx, entry)
		return detailMsg{requestID: requestID, itemID: entry.Item.ID, detail: detail, err: err}
	})
}

func (m Model) recommendDiagnosticsCmd(entry domain.FeedEntry) tea.Cmd {
	return timeoutCmd(defaultCommandTimeout, func(ctx context.Context) tea.Msg {
		diagnostics, err := m.service.RecommendationDiagnostics(ctx, entry)
		return recommendDiagnosticsMsg{diagnostics: diagnostics, err: err}
	})
}

func (m Model) markdownImagePreviewCmd(requestID int, itemID string, key markdownImagePreviewKey, request markdownImagePreviewRequest) tea.Cmd {
	previewer := m.imagePreviewer
	return timeoutCmd(detailCommandTimeout, func(ctx context.Context) tea.Msg {
		if previewer == nil {
			return markdownImagePreviewMsg{requestID: requestID, itemID: itemID, key: key, err: fmt.Errorf("markdown image previewer is not configured")}
		}
		result, err := previewer.RenderMarkdownImage(ctx, request)
		return markdownImagePreviewMsg{requestID: requestID, itemID: itemID, key: key, result: result, err: err}
	})
}

func (m Model) exportSavedCmd(format app.ExportFormat) tea.Cmd {
	return timeoutCmd(exportCommandTimeout, func(ctx context.Context) tea.Msg {
		dir := filepath.Join(m.exportBaseDir(), "exports")
		result, err := m.service.ExportSaved(ctx, app.ExportOptions{Format: format, Dir: dir})
		if err != nil {
			return statusMsg{message: "export failed", err: err}
		}
		return statusMsg{message: fmt.Sprintf("exported saved items: %s (%d)", result.Path, result.Count)}
	})
}

func (m Model) exportBaseDir() string {
	if strings.TrimSpace(m.config.DataDir) == "" {
		return "."
	}
	return m.config.DataDir
}

func (m Model) clearCacheCmd() tea.Cmd {
	return timeoutCmd(exportCommandTimeout, func(ctx context.Context) tea.Msg {
		snapshot, err := m.service.ClearCache(ctx, m.view, m.filter)
		if err != nil {
			return statusMsg{message: "clear cache failed", err: err}
		}
		return snapshotMsg{
			snapshot:    snapshot,
			message:     "cache cleared",
			nextRefresh: &refreshRequest{force: true, mode: app.RefreshModeVisible},
		}
	})
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
			if eventErr := m.recordItemEvent(entry, eventType); eventErr != nil {
				return statusMsg{message: "opened url; event not recorded", err: eventErr}
			}
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
		if err := m.recordItemEvent(entry, eventType); err != nil {
			return statusMsg{message: message + "; event not recorded", toast: toast, err: err}
		}
		return statusMsg{message: message, toast: toast}
	}
}

func (m Model) recordItemEvent(entry domain.FeedEntry, eventType domain.ItemEventType) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.service.RecordItemEvent(ctx, domain.ItemEvent{
		ItemID:    entry.Item.ID,
		EventType: eventType,
		Source:    entry.PrimarySource().Source,
		View:      m.view,
	}); err != nil {
		return fmt.Errorf("record item event: %w", err)
	}
	return nil
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
	requestID int
	itemID    string
	detail    domain.ItemDetail
	err       error
}

type markdownImagePreviewMsg struct {
	requestID int
	itemID    string
	key       markdownImagePreviewKey
	result    markdownImagePreviewResult
	err       error
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

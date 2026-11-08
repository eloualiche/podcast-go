package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func press(t *testing.T, m model, key tea.KeyType) model {
	t.Helper()
	updated, _ := m.Update(tea.KeyMsg{Type: key})
	return updated.(model)
}

func send(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	updated, _ := m.Update(msg)
	return updated.(model)
}

func downloadingModel(t *testing.T) model {
	t.Helper()
	m := initialModel("query", t.TempDir(), ProviderApple)
	m.state = stateSelecting
	m.podcastInfo = PodcastInfo{Name: "Show"}
	m.episodes = []Episode{
		{Title: "One", Selected: true},
		{Title: "Two", Selected: true},
	}
	m = press(t, m, tea.KeyEnter)
	if m.state != stateDownloading {
		t.Fatalf("state = %v, want stateDownloading", m.state)
	}
	return m
}

func TestEscCancelsRunningDownload(t *testing.T) {
	m := downloadingModel(t)
	ctx := m.downloadCtx

	m = press(t, m, tea.KeyEsc)

	if m.state != stateSelecting {
		t.Errorf("state = %v, want stateSelecting", m.state)
	}
	if ctx.Err() == nil {
		t.Error("esc did not cancel the running download")
	}
}

func TestStaleCompletionIsIgnoredAfterEsc(t *testing.T) {
	m := downloadingModel(t)
	stale := downloadCompleteMsg{filename: "one.mp3", gen: m.downloadGen}

	m = press(t, m, tea.KeyEsc)
	m = send(t, m, stale)

	if m.state != stateSelecting {
		t.Errorf("state = %v, want stateSelecting", m.state)
	}
	if len(m.downloaded) != 0 {
		t.Errorf("downloaded = %v, want none", m.downloaded)
	}
}

func TestStaleCompletionDoesNotAdvanceNewRun(t *testing.T) {
	m := downloadingModel(t)
	stale := downloadCompleteMsg{filename: "one.mp3", gen: m.downloadGen}

	m = press(t, m, tea.KeyEsc)
	m = press(t, m, tea.KeyEnter)
	m = send(t, m, stale)

	if m.downloadIndex != 0 {
		t.Errorf("downloadIndex = %d, want 0", m.downloadIndex)
	}
	if len(m.downloaded) != 0 {
		t.Errorf("downloaded = %v, want none", m.downloaded)
	}
}

func TestCurrentCompletionAdvancesToDone(t *testing.T) {
	m := downloadingModel(t)

	m = send(t, m, downloadCompleteMsg{filename: "one.mp3", gen: m.downloadGen})
	if m.downloadIndex != 1 || m.state != stateDownloading {
		t.Errorf("after first: index = %d, state = %v", m.downloadIndex, m.state)
	}

	m = send(t, m, downloadCompleteMsg{filename: "two.mp3", gen: m.downloadGen})
	if m.state != stateDone {
		t.Errorf("after last: state = %v, want stateDone", m.state)
	}
	if len(m.downloaded) != 2 {
		t.Errorf("downloaded = %v, want 2 files", m.downloaded)
	}
}

func TestUncreatableOutputFolderShowsError(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADir, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	m := initialModel("query", notADir, ProviderApple)
	m.state = stateSelecting
	m.podcastInfo = PodcastInfo{Name: "Show"}
	m.episodes = []Episode{{Title: "One", Selected: true}}

	m = press(t, m, tea.KeyEnter)

	if m.state != stateError {
		t.Errorf("state = %v, want stateError", m.state)
	}
}

func TestIDPrefixIsTreatedAsPodcastID(t *testing.T) {
	m := initialModel("id1200361736", ".", ProviderApple)
	if m.podcastID != "id1200361736" || m.searchQuery != "" {
		t.Errorf("podcastID = %q, searchQuery = %q", m.podcastID, m.searchQuery)
	}
}

func TestParseProvider(t *testing.T) {
	tests := []struct {
		input    string
		expected SearchProvider
		wantErr  bool
	}{
		{"apple", ProviderApple, false},
		{"APPLE", ProviderApple, false},
		{"podcastindex", ProviderPodcastIndex, false},
		{"pi", ProviderPodcastIndex, false},
		{"spotify", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseProvider(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseProvider(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if result != tt.expected {
				t.Errorf("parseProvider(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCombineSearchResults(t *testing.T) {
	apple := []SearchResult{{Name: "A", FeedURL: "https://x.test/feed/"}}
	index := []SearchResult{{Name: "A again", FeedURL: "https://X.test/feed"}, {Name: "B", FeedURL: "https://y.test/feed"}}
	failed := errors.New("boom")

	t.Run("both succeed and duplicates are removed", func(t *testing.T) {
		msg := combineSearchResults(apple, nil, index, nil).(searchResultsMsg)
		if len(msg.results) != 2 || msg.warning != "" {
			t.Errorf("results = %v, warning = %q", msg.results, msg.warning)
		}
	})

	t.Run("one provider failing gives a warning", func(t *testing.T) {
		msg := combineSearchResults(apple, nil, nil, failed).(searchResultsMsg)
		if len(msg.results) != 1 {
			t.Errorf("results = %v", msg.results)
		}
		if !strings.Contains(msg.warning, "Podcast Index") || !strings.Contains(msg.warning, "boom") {
			t.Errorf("warning = %q", msg.warning)
		}
	})

	t.Run("both failing is an error", func(t *testing.T) {
		if _, ok := combineSearchResults(nil, failed, nil, failed).(errorMsg); !ok {
			t.Error("want errorMsg")
		}
	})
}

func TestSearchWarningIsShown(t *testing.T) {
	m := initialModel("query", ".", ProviderApple)

	shown := send(t, m, searchResultsMsg{results: []SearchResult{{Name: "A"}}, warning: "Apple search failed"})
	if !strings.Contains(shown.View(), "Apple search failed") {
		t.Error("results view does not show the warning")
	}

	empty := send(t, m, searchResultsMsg{warning: "Apple search failed"})
	if !strings.Contains(empty.errorMsg, "Apple search failed") {
		t.Errorf("errorMsg = %q, should explain the failed search", empty.errorMsg)
	}
}

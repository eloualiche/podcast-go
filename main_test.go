package main

import (
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

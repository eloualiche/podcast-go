package main

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestDownloadSummary(t *testing.T) {
	t.Run("all succeeded", func(t *testing.T) {
		result := downloadSummary(2, "/music/Show", nil)
		if !strings.Contains(result, "Successfully downloaded 2 episode(s)") {
			t.Errorf("summary = %q", result)
		}
	})

	t.Run("some failed", func(t *testing.T) {
		result := downloadSummary(3, "/music/Show", []string{"Two: HTTP 404"})
		if strings.Contains(result, "Successfully") {
			t.Errorf("summary claims success despite a failure: %q", result)
		}
		if !strings.Contains(result, "Downloaded 2 of 3 episode(s)") {
			t.Errorf("summary has wrong count: %q", result)
		}
		if !strings.Contains(result, "Two: HTTP 404") {
			t.Errorf("summary does not name the failure: %q", result)
		}
	})
}

func newTestApp(t *testing.T) *App {
	t.Helper()
	a := &App{outputDir: t.TempDir()}
	a.fyneApp = test.NewApp()
	t.Cleanup(a.fyneApp.Quit)
	a.mainWindow = test.NewWindow(nil)
	a.buildUI()
	return a
}

func TestRecycledRowKeepsPreviousEpisodeSelection(t *testing.T) {
	a := newTestApp(t)
	a.episodes = []Episode{
		{Title: "One", Selected: true},
		{Title: "Two", Selected: false},
	}

	// Fyne reuses a row object for another episode when the list scrolls
	row := a.episodeList.CreateItem()
	a.episodeList.UpdateItem(0, row)
	a.episodeList.UpdateItem(1, row)

	if !a.episodes[0].Selected {
		t.Error("Showing episode Two in a reused row deselected episode One")
	}

	check := row.(*fyne.Container).Objects[1].(*widget.Check)
	check.SetChecked(true)
	if !a.episodes[1].Selected {
		t.Error("Ticking the reused row did not select episode Two")
	}
}

func TestSamePodcastCanBeOpenedAgain(t *testing.T) {
	a := newTestApp(t)
	a.searchResults = []SearchResult{{Name: "Show"}}

	opened := 0
	a.openResult = func(SearchResult) { opened++ }

	a.resultsList.Select(0)
	a.resultsList.Select(0)

	if opened != 2 {
		t.Errorf("Podcast opened %d times, want 2", opened)
	}
}

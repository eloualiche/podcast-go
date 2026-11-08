package main

import (
	"strings"
	"testing"
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

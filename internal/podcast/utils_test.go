package podcast

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsNumeric(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"123456", true},
		{"1200361736", true},
		{"0", true},
		{"", false},
		{"abc", false},
		{"123abc", false},
		{"12.34", false},
		{"-123", false},
		{"id123", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := IsNumeric(tt.input)
			if result != tt.expected {
				t.Errorf("IsNumeric(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestAudioExtension(t *testing.T) {
	tests := []struct {
		name     string
		mime     string
		url      string
		expected string
	}{
		// MIME-driven (authoritative when present)
		{"audio/mpeg", "audio/mpeg", "https://example.com/ep1", ".mp3"},
		{"audio/mp3", "audio/mp3", "https://example.com/ep1", ".mp3"},
		{"audio/mp4", "audio/mp4", "https://example.com/ep1", ".m4a"},
		{"audio/x-m4a", "audio/x-m4a", "https://example.com/ep1", ".m4a"},
		{"audio/aac", "audio/aac", "https://example.com/ep1", ".m4a"},
		{"audio/ogg", "audio/ogg", "https://example.com/ep1", ".ogg"},
		{"audio/opus", "audio/opus", "https://example.com/ep1", ".opus"},
		{"audio/wav", "audio/wav", "https://example.com/ep1", ".wav"},
		{"audio/flac", "audio/flac", "https://example.com/ep1", ".flac"},
		{"uppercase mime", "AUDIO/MPEG", "https://example.com/ep1", ".mp3"},
		{"mime with charset", "audio/mp4; codecs=mp4a.40.2", "https://example.com/ep1", ".m4a"},

		// URL fallback when MIME absent or generic
		{"empty mime, mp3 url", "", "https://example.com/show/ep.mp3", ".mp3"},
		{"empty mime, m4a url", "", "https://example.com/show/ep.m4a", ".m4a"},
		{"empty mime, mp4 url", "", "https://example.com/show/ep.mp4", ".m4a"},
		{"empty mime, opus url", "", "https://example.com/show/ep.opus", ".opus"},
		{"url with query string", "", "https://example.com/show/ep.m4a?t=12345&token=abc", ".m4a"},
		{"url with fragment", "", "https://example.com/show/ep.mp3#start", ".mp3"},
		{"uppercase url", "", "https://example.com/SHOW/EP.M4A", ".m4a"},

		// Defaults
		{"unknown mime, no ext url", "application/octet-stream", "https://example.com/track/123", ".mp3"},
		{"both empty", "", "", ".mp3"},
		{"mime wins over url", "audio/mp4", "https://example.com/ep.mp3", ".m4a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AudioExtension(tt.mime, tt.url)
			if got != tt.expected {
				t.Errorf("AudioExtension(%q, %q) = %q, want %q", tt.mime, tt.url, got, tt.expected)
			}
		})
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Simple Title", "Simple Title"},
		{"Title: With Colon", "Title With Colon"},
		{"Title/With/Slashes", "TitleWithSlashes"},
		{"Title\\With\\Backslashes", "TitleWithBackslashes"},
		{"Title<With>Brackets", "TitleWithBrackets"},
		{"Title|With|Pipes", "TitleWithPipes"},
		{"Title?With?Questions", "TitleWithQuestions"},
		{"Title*With*Stars", "TitleWithStars"},
		{"Title\"With\"Quotes", "TitleWithQuotes"},
		{"  Spaces Around  ", "Spaces Around"},
		{"", "episode"},
		{"   ", "episode"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := SanitizeFilename(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeFilename(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSanitizeFilename_LongName(t *testing.T) {
	longName := strings.Repeat("a", 150)
	result := SanitizeFilename(longName)
	if len(result) > 100 {
		t.Errorf("SanitizeFilename should truncate to 100 chars, got %d", len(result))
	}
	if len(result) != 100 {
		t.Errorf("SanitizeFilename(%d chars) = %d chars, want 100", len(longName), len(result))
	}
}

func TestDownloadFile_TextRedirect(t *testing.T) {
	// Mock server that returns a text-based redirect
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("fake audio content"))
	}))
	defer redirectServer.Close()

	mainServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Redirecting to " + redirectServer.URL))
	}))
	defer mainServer.Close()

	// Create temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.mp3")

	err := DownloadFile(tmpFile, mainServer.URL, nil)
	if err != nil {
		t.Fatalf("DownloadFile failed: %v", err)
	}

	// Verify file was created with correct content
	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(content) != "fake audio content" {
		t.Errorf("File content = %q, want %q", string(content), "fake audio content")
	}
}

func TestDownloadFile_HTMLEntityDecode(t *testing.T) {
	// Mock server that returns a text-based redirect with HTML entities
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the query params were properly decoded
		if r.URL.Query().Get("foo") != "bar" {
			t.Errorf("Query param foo = %q, want %q", r.URL.Query().Get("foo"), "bar")
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("audio with params"))
	}))
	defer redirectServer.Close()

	mainServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		// Simulate Acast's HTML-encoded ampersands
		w.Write([]byte("Redirecting to " + redirectServer.URL + "?foo=bar&amp;baz=qux"))
	}))
	defer mainServer.Close()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.mp3")

	err := DownloadFile(tmpFile, mainServer.URL, nil)
	if err != nil {
		t.Fatalf("DownloadFile failed: %v", err)
	}

	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(content) != "audio with params" {
		t.Errorf("File content = %q, want %q", string(content), "audio with params")
	}
}

func TestDownloadFile_DirectDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Content-Length", "13")
		w.Write([]byte("audio content"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.mp3")

	err := DownloadFile(tmpFile, server.URL, nil)
	if err != nil {
		t.Fatalf("DownloadFile failed: %v", err)
	}

	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(content) != "audio content" {
		t.Errorf("File content = %q, want %q", string(content), "audio content")
	}
}

func TestDownloadFile_SkipExisting(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Write([]byte("new content"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "existing.mp3")

	// Create existing file
	err := os.WriteFile(tmpFile, []byte("original content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	err = DownloadFile(tmpFile, server.URL, nil)
	if err != nil {
		t.Fatalf("DownloadFile failed: %v", err)
	}

	// Should not have made a request
	if callCount != 0 {
		t.Errorf("Server was called %d times, want 0 (should skip existing)", callCount)
	}

	// Content should be unchanged
	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(content) != "original content" {
		t.Errorf("File content = %q, want %q", string(content), "original content")
	}
}

func TestDownloadFile_ProgressCallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Content-Length", "100")
		// Write 100 bytes
		w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.mp3")

	var lastProgress float64
	progressCalled := false

	err := DownloadFile(tmpFile, server.URL, func(percent float64) {
		progressCalled = true
		lastProgress = percent
	})
	if err != nil {
		t.Fatalf("DownloadFile failed: %v", err)
	}

	if !progressCalled {
		t.Error("Progress callback was never called")
	}

	if lastProgress < 0.99 {
		t.Errorf("Last progress = %v, want >= 0.99", lastProgress)
	}
}

func TestParseEpisodeSpec(t *testing.T) {
	tests := []struct {
		spec     string
		total    int
		expected []int
	}{
		{"", 5, []int{1, 2, 3, 4, 5}},
		{"all", 3, []int{1, 2, 3}},
		{"latest", 10, []int{1}},
		{"1", 10, []int{1}},
		{"5", 10, []int{5}},
		{"1,3,5", 10, []int{1, 3, 5}},
		{"1-3", 10, []int{1, 2, 3}},
		{"1-5", 10, []int{1, 2, 3, 4, 5}},
		{"1,3-5,7", 10, []int{1, 3, 4, 5, 7}},
		{"1, 2, 3", 10, []int{1, 2, 3}}, // spaces
	}

	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			result := ParseEpisodeSpec(tt.spec, tt.total)
			if len(result) != len(tt.expected) {
				t.Errorf("ParseEpisodeSpec(%q, %d) = %v, want %v", tt.spec, tt.total, result, tt.expected)
				return
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("ParseEpisodeSpec(%q, %d) = %v, want %v", tt.spec, tt.total, result, tt.expected)
					break
				}
			}
		})
	}
}

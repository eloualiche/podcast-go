package podcast

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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

func TestSanitizeFilename_TruncatesOnRuneBoundary(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"2-byte rune straddles limit", strings.Repeat("a", 99) + "» suite", strings.Repeat("a", 99)},
		{"3-byte rune straddles limit", strings.Repeat("a", 98) + "’ suite", strings.Repeat("a", 98)},
		{"4-byte rune straddles limit", strings.Repeat("a", 97) + "🎙 suite", strings.Repeat("a", 97)},
		{"rune ends exactly at limit", strings.Repeat("a", 98) + "» suite", strings.Repeat("a", 98) + "»"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeFilename(tt.input)
			if !utf8.ValidString(result) {
				t.Errorf("SanitizeFilename(%q) = %q, not valid UTF-8", tt.input, result)
			}
			if result != tt.expected {
				t.Errorf("SanitizeFilename(%q) = %q, want %q", tt.input, result, tt.expected)
			}
			if err := os.WriteFile(filepath.Join(t.TempDir(), result+".mp3"), nil, 0o644); err != nil {
				t.Errorf("cannot create file from sanitized name: %v", err)
			}
		})
	}
}

func TestEpisodeFilename(t *testing.T) {
	published := time.Date(2026, 7, 12, 18, 50, 0, 0, time.UTC)

	tests := []struct {
		name     string
		pubDate  time.Time
		title    string
		ext      string
		expected string
	}{
		{"dated", published, "The Sunday Read", ".mp3", "2026-07-12 - The Sunday Read.mp3"},
		{"no date", time.Time{}, "The Sunday Read", ".mp3", "The Sunday Read.mp3"},
		{"sanitizes title", published, "Why: A/B?", ".m4a", "2026-07-12 - Why AB.m4a"},
		{"empty title", published, "", ".mp3", "2026-07-12 - episode.mp3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EpisodeFilename(tt.pubDate, tt.title, tt.ext)
			if result != tt.expected {
				t.Errorf("EpisodeFilename(%v, %q, %q) = %q, want %q", tt.pubDate, tt.title, tt.ext, result, tt.expected)
			}
		})
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

	err := DownloadFile(context.Background(), tmpFile, mainServer.URL, nil)
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

	err := DownloadFile(context.Background(), tmpFile, mainServer.URL, nil)
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

	err := DownloadFile(context.Background(), tmpFile, server.URL, nil)
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

	err = DownloadFile(context.Background(), tmpFile, server.URL, nil)
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

func TestDownloadFile_InterruptedLeavesNoFileAndRetries(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Content-Length", "13")
		if callCount == 1 {
			// Connection drops before the promised length is sent
			w.Write([]byte("audio"))
			return
		}
		w.Write([]byte("audio content"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.mp3")

	if err := DownloadFile(context.Background(), tmpFile, server.URL, nil); err == nil {
		t.Fatal("DownloadFile succeeded on an interrupted download, want error")
	}
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Interrupted download left %q behind, want empty dir", entries[0].Name())
	}

	if err := DownloadFile(context.Background(), tmpFile, server.URL, nil); err != nil {
		t.Fatalf("Retry failed: %v", err)
	}
	if callCount != 2 {
		t.Errorf("Server was called %d times, want 2 (retry should download again)", callCount)
	}
	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if string(content) != "audio content" {
		t.Errorf("File content = %q, want %q", string(content), "audio content")
	}
}

func TestDownloadFile_TextBodyIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("not found"))
	}))
	defer server.Close()

	tmpFile := filepath.Join(t.TempDir(), "test.mp3")

	err := DownloadFile(context.Background(), tmpFile, server.URL, nil)
	if err == nil {
		t.Fatal("DownloadFile succeeded on a text response, want error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Error %q should include the response text", err)
	}
	if _, err := os.Stat(tmpFile); err == nil {
		t.Error("Text response created an audio file")
	}
}

func TestDownloadFile_CancelStopsAndCleansUp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Content-Length", "100")
		w.Write([]byte(strings.Repeat("x", 50)))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := DownloadFile(ctx, filepath.Join(tmpDir, "test.mp3"), server.URL, func(float64) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Cancelled download left %q behind, want empty dir", entries[0].Name())
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write(p []byte) (int, error) { return 0, w.err }

func TestCopyWithProgress_ReportsWriteError(t *testing.T) {
	diskFull := errors.New("no space left on device")

	err := copyWithProgress(failingWriter{diskFull}, strings.NewReader("audio"), 5, nil)
	if !errors.Is(err, diskFull) {
		t.Errorf("copyWithProgress error = %v, want %v", err, diskFull)
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

	err := DownloadFile(context.Background(), tmpFile, server.URL, func(percent float64) {
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

func TestAppleSearchURL(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected string
	}{
		{"spaces", "the daily", "term=the+daily&"},
		{"ampersand", "Q&A", "term=Q%26A&"},
		{"hash", "C# weekly", "term=C%23+weekly&"},
		{"accents", "économie", "term=%C3%A9conomie&"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AppleSearchURL(tt.query)
			if !strings.HasPrefix(result, "https://itunes.apple.com/search?") {
				t.Errorf("AppleSearchURL(%q) = %q, wrong endpoint", tt.query, result)
			}
			if !strings.Contains(result, tt.expected) {
				t.Errorf("AppleSearchURL(%q) = %q, want it to contain %q", tt.query, result, tt.expected)
			}
			if !strings.Contains(result, "media=podcast") {
				t.Errorf("AppleSearchURL(%q) = %q, lost media=podcast", tt.query, result)
			}
		})
	}
}

func setStallTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	original := stallTimeout
	stallTimeout = d
	t.Cleanup(func() { stallTimeout = original })
}

func TestDownloadFile_StalledBodyFails(t *testing.T) {
	setStallTimeout(t, 50*time.Millisecond)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Content-Length", "100")
		w.Write([]byte(strings.Repeat("x", 50)))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	err := DownloadFile(context.Background(), filepath.Join(tmpDir, "test.mp3"), server.URL, nil)
	if !errors.Is(err, errDownloadStalled) {
		t.Errorf("error = %v, want errDownloadStalled", err)
	}
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Stalled download left %q behind, want empty dir", entries[0].Name())
	}
}

func TestDownloadFile_NoResponseFails(t *testing.T) {
	setStallTimeout(t, 50*time.Millisecond)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	err := DownloadFile(context.Background(), filepath.Join(t.TempDir(), "test.mp3"), server.URL, nil)
	if !errors.Is(err, errDownloadStalled) {
		t.Errorf("error = %v, want errDownloadStalled", err)
	}
}

func TestDownloadFile_SlowButSteadySucceeds(t *testing.T) {
	setStallTimeout(t, 200*time.Millisecond)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Content-Length", "6")
		for i := 0; i < 6; i++ {
			w.Write([]byte("x"))
			w.(http.Flusher).Flush()
			time.Sleep(60 * time.Millisecond)
		}
	}))
	defer server.Close()

	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	if err := DownloadFile(context.Background(), tmpFile, server.URL, nil); err != nil {
		t.Errorf("Slow but steady download failed: %v", err)
	}
}

func TestDownloadFile_RedirectTargetOutlastsFirstTimer(t *testing.T) {
	setStallTimeout(t, 200*time.Millisecond)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Content-Length", "6")
		for i := 0; i < 6; i++ {
			w.Write([]byte("x"))
			w.(http.Flusher).Flush()
			time.Sleep(60 * time.Millisecond)
		}
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Redirecting to " + target.URL))
	}))
	defer redirect.Close()

	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	if err := DownloadFile(context.Background(), tmpFile, redirect.URL, nil); err != nil {
		t.Errorf("Redirected download failed: %v", err)
	}
}

func TestSanitizeFilename_UnsafeNames(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"parent directory", "..", "episode"},
		{"current directory", ".", "episode"},
		{"only dots", "...", "episode"},
		{"dots after stripping", ".:.", "episode"},
		{"keeps inner dots", "Vol. 2...", "Vol. 2..."},
		{"newline", "Part 1\nThe Sequel", "Part 1 The Sequel"},
		{"tab and NUL", "A\tB\x00C", "A B C"},
		{"terminal escape", "A\x1b[31mB", "A [31mB"},
		{"invalid UTF-8", "caf\xe9 noir", "caf noir"},
		{"space left by cut", strings.Repeat("a", 98) + " » suite", strings.Repeat("a", 98)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeFilename(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeFilename(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		max      int
		expected string
	}{
		{"short", "abc", 5, "abc"},
		{"exact", "abcde", 5, "abcde"},
		{"long", "abcdefgh", 5, "ab..."},
		{"counts characters not bytes", "ééééé", 5, "ééééé"},
		{"cuts between characters", "éééééééé", 5, "éé..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Truncate(tt.input, tt.max)
			if result != tt.expected {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.input, tt.max, result, tt.expected)
			}
		})
	}
}

func TestIsPodcastID(t *testing.T) {
	tests := map[string]bool{
		"1200361736":   true,
		"id1200361736": true,
		"ID1200361736": true,
		"id":           false,
		"idea":         false,
		"the daily":    false,
		"":             false,
	}

	for input, expected := range tests {
		if result := IsPodcastID(input); result != expected {
			t.Errorf("IsPodcastID(%q) = %v, want %v", input, result, expected)
		}
	}
}

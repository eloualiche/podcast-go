package podcast

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// UserAgent is used for HTTP requests. Some podcast hosts (like Acast) block
// Go's default User-Agent.
const UserAgent = "PodcastDownloader/1.0"

// httpClient is a shared HTTP client with proper User-Agent
var httpClient = &http.Client{}

// MetadataClient is for search, lookup and feed requests. Downloads use
// stallTimeout instead, since a large episode can take much longer than this.
var MetadataClient = &http.Client{Timeout: 60 * time.Second}

// stallTimeout is how long a download may go without receiving any data
var stallTimeout = 60 * time.Second

var errDownloadStalled = errors.New("download stalled: no data received from server")

// httpGet performs an HTTP GET with proper User-Agent
func httpGet(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	return httpClient.Do(req)
}

// IsNumeric checks if a string is all digits (podcast ID)
func IsNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

// AudioExtension returns a file extension (with leading dot, lowercase) for
// a podcast audio enclosure based on its MIME type and URL. The MIME type is
// preferred when present; otherwise the URL path is inspected (query strings
// stripped). When neither yields a recognised audio container, ".mp3" is
// returned as a safe default — most podcasts are MPEG audio and downstream
// code expects that extension.
//
// Recognising the real container matters because writing an ID3v2 tag onto a
// non-MPEG file (e.g. M4A/MP4) prepends bytes the container parser does not
// expect, corrupting the file.
func AudioExtension(mimeType, url string) string {
	mt := strings.ToLower(strings.TrimSpace(mimeType))
	switch {
	case strings.Contains(mt, "mpeg"), strings.Contains(mt, "mp3"):
		return ".mp3"
	case strings.Contains(mt, "mp4"), strings.Contains(mt, "m4a"), strings.Contains(mt, "aac"):
		return ".m4a"
	case strings.Contains(mt, "opus"):
		return ".opus"
	case strings.Contains(mt, "ogg"), strings.Contains(mt, "vorbis"):
		return ".ogg"
	case strings.Contains(mt, "wav"):
		return ".wav"
	case strings.Contains(mt, "flac"):
		return ".flac"
	}

	u := strings.ToLower(url)
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	switch {
	case strings.HasSuffix(u, ".mp3"):
		return ".mp3"
	case strings.HasSuffix(u, ".m4a"), strings.HasSuffix(u, ".mp4"),
		strings.HasSuffix(u, ".m4b"), strings.HasSuffix(u, ".aac"):
		return ".m4a"
	case strings.HasSuffix(u, ".opus"):
		return ".opus"
	case strings.HasSuffix(u, ".ogg"), strings.HasSuffix(u, ".oga"):
		return ".ogg"
	case strings.HasSuffix(u, ".wav"):
		return ".wav"
	case strings.HasSuffix(u, ".flac"):
		return ".flac"
	}
	return ".mp3"
}

// SanitizeFilename removes invalid characters from a filename
func SanitizeFilename(name string) string {
	// Remove invalid characters
	re := regexp.MustCompile(`[<>:"/\\|?*]`)
	name = re.ReplaceAllString(name, "")
	name = strings.ToValidUTF8(name, "")
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, name)

	// Limit length, backing up to a rune boundary: a split multi-byte
	// character is invalid UTF-8, which macOS rejects in filenames
	name = strings.TrimSpace(name)
	if len(name) > 100 {
		cut := 100
		for cut > 0 && !utf8.RuneStart(name[cut]) {
			cut--
		}
		name = strings.TrimSpace(name[:cut])
	}

	// "." and ".." would resolve to an existing directory rather than a name
	if strings.Trim(name, ".") == "" {
		return "episode"
	}
	return name
}

// AppleSearchURL builds the iTunes Search API URL for a podcast search
func AppleSearchURL(query string) string {
	return "https://itunes.apple.com/search?term=" + neturl.QueryEscape(query) + "&media=podcast&limit=25"
}

// EpisodeFilename builds the filename for an episode. It is prefixed with the
// publish date rather than the feed position, which shifts with every new
// episode and would make an already downloaded file go unrecognised.
func EpisodeFilename(pubDate time.Time, title, ext string) string {
	name := SanitizeFilename(title) + ext
	if pubDate.IsZero() {
		return name
	}
	return pubDate.Format("2006-01-02") + " - " + name
}

// ProgressCallback is called during downloads with the current progress (0.0-1.0)
type ProgressCallback func(percent float64)

// DownloadFile downloads a file from a URL with progress reporting
// It handles text-based redirects used by some podcast hosts (like Acast)
// Cancelling ctx stops the download and removes the unfinished file
func DownloadFile(ctx context.Context, filepath string, url string, onProgress ProgressCallback) error {
	// Check if already exists
	if _, err := os.Stat(filepath); err == nil {
		return nil
	}

	// Give up if no data arrives for stallTimeout. A fixed overall deadline
	// would cut off large episodes on slow connections.
	dlCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	timer := time.AfterFunc(stallTimeout, func() { cancel(errDownloadStalled) })
	defer timer.Stop()

	resp, err := httpGet(dlCtx, url)
	if err != nil {
		return downloadError(dlCtx, err)
	}
	defer resp.Body.Close()

	// Check for HTTP errors
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Some podcast hosts (like Acast) return a 200 with text body containing redirect URL
	// instead of using proper HTTP redirects. Detect and follow these.
	if resp.ContentLength > 0 && resp.ContentLength < 1000 &&
		strings.Contains(resp.Header.Get("Content-Type"), "text/plain") {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return downloadError(dlCtx, err)
		}
		bodyStr := string(body)
		if strings.HasPrefix(bodyStr, "Redirecting to ") {
			redirectURL := strings.TrimPrefix(bodyStr, "Redirecting to ")
			redirectURL = strings.TrimSpace(redirectURL)
			// Unescape HTML entities like &amp; -> &
			redirectURL = html.UnescapeString(redirectURL)
			return DownloadFile(ctx, filepath, redirectURL, onProgress)
		}
		return fmt.Errorf("expected audio, got text response: %q", bodyStr)
	}

	// Write to a temporary name and rename once complete, so that the skip
	// check above never mistakes an interrupted download for a finished one
	partPath := filepath + ".part"
	out, err := os.Create(partPath)
	if err != nil {
		return err
	}

	body := stallReader{resp.Body, timer}
	err = copyWithProgress(out, body, resp.ContentLength, onProgress)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(partPath)
		return downloadError(dlCtx, err)
	}

	return os.Rename(partPath, filepath)
}

// downloadError reports why ctx ended, if it did, rather than the less
// helpful error that the interrupted request returned
func downloadError(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return err
}

// stallReader restarts the stall timer whenever data arrives
type stallReader struct {
	r     io.Reader
	timer *time.Timer
}

func (s stallReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.timer.Reset(stallTimeout)
	}
	return n, err
}

// copyWithProgress copies body to out, reporting progress against totalSize
func copyWithProgress(out io.Writer, body io.Reader, totalSize int64, onProgress ProgressCallback) error {
	downloaded := int64(0)
	lastPercent := float64(0)

	buf := make([]byte, 32*1024)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			downloaded += int64(n)
			if totalSize > 0 && onProgress != nil {
				percent := float64(downloaded) / float64(totalSize)
				// Only send updates every 1% to avoid flooding
				if percent-lastPercent >= 0.01 || percent >= 1.0 {
					lastPercent = percent
					onProgress(percent)
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// ParseEpisodeSpec parses an episode specification string and returns episode indices.
// Supports: "all", "latest", single numbers "5", ranges "1-5", and comma-separated "1,3,5"
func ParseEpisodeSpec(spec string, total int) []int {
	if spec == "" || spec == "all" {
		result := make([]int, total)
		for i := range result {
			result[i] = i + 1
		}
		return result
	}

	if spec == "latest" {
		return []int{1}
	}

	var result []int
	parts := strings.Split(spec, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) == 2 {
				start := parseInt(strings.TrimSpace(rangeParts[0]))
				end := parseInt(strings.TrimSpace(rangeParts[1]))
				if start > 0 && end > 0 {
					for i := start; i <= end; i++ {
						result = append(result, i)
					}
				}
			}
		} else {
			if num := parseInt(part); num > 0 {
				result = append(result, num)
			}
		}
	}
	return result
}

func parseInt(s string) int {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

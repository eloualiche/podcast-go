package podcast

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
)

// UserAgent is used for HTTP requests. Some podcast hosts (like Acast) block
// Go's default User-Agent.
const UserAgent = "PodcastDownloader/1.0"

// httpClient is a shared HTTP client with proper User-Agent
var httpClient = &http.Client{}

// httpGet performs an HTTP GET with proper User-Agent
func httpGet(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
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
	name = strings.TrimSpace(name)

	// Limit length
	if len(name) > 100 {
		name = name[:100]
	}

	if name == "" {
		return "episode"
	}
	return name
}

// ProgressCallback is called during downloads with the current progress (0.0-1.0)
type ProgressCallback func(percent float64)

// DownloadFile downloads a file from a URL with progress reporting
// It handles text-based redirects used by some podcast hosts (like Acast)
func DownloadFile(filepath string, url string, onProgress ProgressCallback) error {
	// Check if already exists
	if _, err := os.Stat(filepath); err == nil {
		return nil
	}

	resp, err := httpGet(url)
	if err != nil {
		return err
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
			return err
		}
		bodyStr := string(body)
		if strings.HasPrefix(bodyStr, "Redirecting to ") {
			redirectURL := strings.TrimPrefix(bodyStr, "Redirecting to ")
			redirectURL = strings.TrimSpace(redirectURL)
			// Unescape HTML entities like &amp; -> &
			redirectURL = html.UnescapeString(redirectURL)
			return DownloadFile(filepath, redirectURL, onProgress)
		}
	}

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	totalSize := resp.ContentLength
	downloaded := int64(0)
	lastPercent := float64(0)

	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
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

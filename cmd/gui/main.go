package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/bogem/id3v2"
	"github.com/mmcdole/gofeed"
)

// Data structures (shared with TUI)

type PodcastInfo struct {
	Name       string
	Artist     string
	FeedURL    string
	ArtworkURL string
	ID         string
}

type SearchResult struct {
	ID         string
	Name       string
	Artist     string
	FeedURL    string
	ArtworkURL string
	Source     SearchProvider
}

type Episode struct {
	Index       int
	Title       string
	Description string
	AudioURL    string
	PubDate     time.Time
	Duration    string
	Selected    bool
}

type iTunesResponse struct {
	ResultCount int `json:"resultCount"`
	Results     []struct {
		CollectionID   int    `json:"collectionId"`
		CollectionName string `json:"collectionName"`
		ArtistName     string `json:"artistName"`
		FeedURL        string `json:"feedUrl"`
		ArtworkURL600  string `json:"artworkUrl600"`
		ArtworkURL100  string `json:"artworkUrl100"`
	} `json:"results"`
}

type podcastIndexResponse struct {
	Status string `json:"status"`
	Feeds  []struct {
		ID          int    `json:"id"`
		Title       string `json:"title"`
		Author      string `json:"author"`
		URL         string `json:"url"`
		Image       string `json:"image"`
		Description string `json:"description"`
	} `json:"feeds"`
	Count int `json:"count"`
}

type SearchProvider string

const (
	ProviderApple        SearchProvider = "apple"
	ProviderPodcastIndex SearchProvider = "podcastindex"
)

// App holds the application state
type App struct {
	fyneApp    fyne.App
	mainWindow fyne.Window

	// UI components
	searchEntry    *widget.Entry
	searchButton   *widget.Button
	resultsList    *widget.List
	episodeList    *widget.List
	progressBar    *widget.ProgressBar
	statusLabel    *widget.Label
	downloadButton *widget.Button
	selectAllCheck *widget.Check
	backButton     *widget.Button
	outputDirEntry *widget.Entry
	browseButton   *widget.Button

	// Containers for switching views
	mainContainer    *fyne.Container
	searchView       *fyne.Container
	episodeView      *fyne.Container
	downloadView     *fyne.Container

	// Header label for episode view
	podcastHeader *widget.Label

	// Data
	searchResults  []SearchResult
	episodes       []Episode
	podcastInfo    PodcastInfo
	outputDir      string
	downloading    bool
}

func main() {
	podApp := &App{
		outputDir: ".",
	}
	podApp.Run()
}

func (a *App) Run() {
	a.fyneApp = app.New()
	a.mainWindow = a.fyneApp.NewWindow("Podcast Downloader")
	a.mainWindow.Resize(fyne.NewSize(800, 600))

	a.buildUI()
	a.showSearchView()

	a.mainWindow.ShowAndRun()
}

func (a *App) buildUI() {
	// Search view components
	a.searchEntry = widget.NewEntry()
	a.searchEntry.SetPlaceHolder("Search podcasts or enter Apple Podcast ID...")
	a.searchEntry.OnSubmitted = func(_ string) { a.doSearch() }

	a.searchButton = widget.NewButtonWithIcon("Search", theme.SearchIcon(), a.doSearch)

	a.resultsList = widget.NewList(
		func() int { return len(a.searchResults) },
		func() fyne.CanvasObject {
			return container.NewVBox(
				widget.NewLabel("Podcast Name"),
				widget.NewLabel("Artist"),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(a.searchResults) {
				return
			}
			result := a.searchResults[id]
			vbox := obj.(*fyne.Container)
			nameLabel := vbox.Objects[0].(*widget.Label)
			artistLabel := vbox.Objects[1].(*widget.Label)
			nameLabel.SetText(result.Name)
			sourceTag := ""
			if result.Source == ProviderPodcastIndex {
				sourceTag = " [PI]"
			}
			artistLabel.SetText(result.Artist + sourceTag)
		},
	)
	a.resultsList.OnSelected = func(id widget.ListItemID) {
		if id < len(a.searchResults) {
			a.loadPodcast(a.searchResults[id])
		}
	}

	// Output directory selection
	a.outputDirEntry = widget.NewEntry()
	a.outputDirEntry.SetText(a.outputDir)
	a.outputDirEntry.OnChanged = func(s string) { a.outputDir = s }

	a.browseButton = widget.NewButtonWithIcon("Browse", theme.FolderOpenIcon(), func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil || uri == nil {
				return
			}
			a.outputDir = uri.Path()
			a.outputDirEntry.SetText(a.outputDir)
		}, a.mainWindow)
	})

	outputRow := container.NewBorder(nil, nil, widget.NewLabel("Output:"), a.browseButton, a.outputDirEntry)

	searchRow := container.NewBorder(nil, nil, nil, a.searchButton, a.searchEntry)

	a.searchView = container.NewBorder(
		container.NewVBox(
			widget.NewLabel("Podcast Downloader"),
			searchRow,
			outputRow,
			widget.NewSeparator(),
		),
		nil, nil, nil,
		a.resultsList,
	)

	// Episode view components
	a.backButton = widget.NewButtonWithIcon("Back", theme.NavigateBackIcon(), func() {
		a.showSearchView()
	})

	a.selectAllCheck = widget.NewCheck("Select All", func(checked bool) {
		for i := range a.episodes {
			a.episodes[i].Selected = checked
		}
		a.episodeList.Refresh()
		a.updateDownloadButton()
	})

	a.episodeList = widget.NewList(
		func() int { return len(a.episodes) },
		func() fyne.CanvasObject {
			check := widget.NewCheck("", nil)
			titleLabel := widget.NewLabel("Episode Title")
			dateLabel := widget.NewLabel("2024-01-01")
			durationLabel := widget.NewLabel("00:00")
			return container.NewBorder(
				nil, nil,
				check,
				container.NewHBox(dateLabel, durationLabel),
				titleLabel,
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(a.episodes) {
				return
			}
			ep := a.episodes[id]
			border := obj.(*fyne.Container)
			check := border.Objects[1].(*widget.Check)
			titleLabel := border.Objects[0].(*widget.Label)
			rightBox := border.Objects[2].(*fyne.Container)
			dateLabel := rightBox.Objects[0].(*widget.Label)
			durationLabel := rightBox.Objects[1].(*widget.Label)

			check.SetChecked(ep.Selected)
			check.OnChanged = func(checked bool) {
				a.episodes[id].Selected = checked
				a.updateDownloadButton()
			}

			title := ep.Title
			if len(title) > 60 {
				title = title[:57] + "..."
			}
			titleLabel.SetText(fmt.Sprintf("[%d] %s", ep.Index, title))

			if !ep.PubDate.IsZero() {
				dateLabel.SetText(ep.PubDate.Format("2006-01-02"))
			} else {
				dateLabel.SetText("")
			}
			durationLabel.SetText(ep.Duration)
		},
	)

	a.downloadButton = widget.NewButtonWithIcon("Download Selected", theme.DownloadIcon(), a.startDownload)
	a.downloadButton.Importance = widget.HighImportance

	a.podcastHeader = widget.NewLabel("")
	a.podcastHeader.TextStyle = fyne.TextStyle{Bold: true}

	a.episodeView = container.NewBorder(
		container.NewVBox(
			container.NewHBox(a.backButton, a.podcastHeader),
			widget.NewSeparator(),
			a.selectAllCheck,
		),
		container.NewVBox(
			widget.NewSeparator(),
			container.NewHBox(a.downloadButton),
		),
		nil, nil,
		a.episodeList,
	)

	// Download view components
	a.progressBar = widget.NewProgressBar()
	a.statusLabel = widget.NewLabel("Ready")

	a.downloadView = container.NewVBox(
		widget.NewLabel("Downloading..."),
		a.progressBar,
		a.statusLabel,
	)

	// Main container with all views
	a.mainContainer = container.NewStack(a.searchView, a.episodeView, a.downloadView)
	a.mainWindow.SetContent(a.mainContainer)
}

func (a *App) showSearchView() {
	a.searchView.Show()
	a.episodeView.Hide()
	a.downloadView.Hide()
}

func (a *App) showEpisodeView() {
	a.searchView.Hide()
	a.episodeView.Show()
	a.downloadView.Hide()

	// Update header
	a.podcastHeader.SetText(fmt.Sprintf("%s - %d episodes", a.podcastInfo.Name, len(a.episodes)))
}

func (a *App) showDownloadView() {
	a.searchView.Hide()
	a.episodeView.Hide()
	a.downloadView.Show()
}

func (a *App) updateDownloadButton() {
	count := 0
	for _, ep := range a.episodes {
		if ep.Selected {
			count++
		}
	}
	if count > 0 {
		a.downloadButton.SetText(fmt.Sprintf("Download %d Episode(s)", count))
		a.downloadButton.Enable()
	} else {
		a.downloadButton.SetText("Download Selected")
		a.downloadButton.Disable()
	}
}

func (a *App) doSearch() {
	query := strings.TrimSpace(a.searchEntry.Text)
	if query == "" {
		return
	}

	a.searchButton.Disable()
	a.statusLabel.SetText("Searching...")

	go func() {
		var results []SearchResult
		var err error

		if isNumeric(query) {
			// Direct podcast ID lookup
			info, episodes, loadErr := loadPodcastByID(query)
			if loadErr != nil {
				fyne.Do(func() {
					a.showError("Failed to load podcast", loadErr)
					a.searchButton.Enable()
				})
				return
			}
			fyne.Do(func() {
				a.podcastInfo = info
				a.episodes = episodes
				a.searchButton.Enable()
				a.episodeList.Refresh()
				a.updateDownloadButton()
				a.showEpisodeView()
			})
			return
		}

		// Search both sources if credentials available
		if hasPodcastIndexCredentials() {
			results, err = searchBoth(query)
		} else {
			results, err = searchAppleResults(query)
		}

		if err != nil {
			fyne.Do(func() {
				a.showError("Search failed", err)
				a.searchButton.Enable()
			})
			return
		}

		fyne.Do(func() {
			a.searchResults = results
			a.resultsList.Refresh()
			a.searchButton.Enable()
			a.statusLabel.SetText(fmt.Sprintf("Found %d podcasts", len(results)))
		})
	}()
}

func (a *App) loadPodcast(result SearchResult) {
	a.statusLabel.SetText(fmt.Sprintf("Loading %s...", result.Name))

	go func() {
		var info PodcastInfo
		var episodes []Episode
		var err error

		if result.Source == ProviderPodcastIndex {
			info, episodes, err = loadPodcastFromFeed(result.FeedURL, result.Name, result.Artist, result.ArtworkURL)
		} else {
			info, episodes, err = loadPodcastByID(result.ID)
		}

		if err != nil {
			fyne.Do(func() {
				a.showError("Failed to load podcast", err)
			})
			return
		}

		fyne.Do(func() {
			a.podcastInfo = info
			a.episodes = episodes
			a.selectAllCheck.SetChecked(false)
			a.episodeList.Refresh()
			a.updateDownloadButton()
			a.showEpisodeView()
			a.statusLabel.SetText("Ready")
		})
	}()
}

func (a *App) startDownload() {
	if a.downloading {
		return
	}

	selected := a.getSelectedEpisodes()
	if len(selected) == 0 {
		return
	}

	a.downloading = true
	a.showDownloadView()

	// Create output directory
	podcastFolder := sanitizeFilename(a.podcastInfo.Name)
	outputDir := filepath.Join(a.outputDir, podcastFolder)
	os.MkdirAll(outputDir, 0755)

	go func() {
		defer func() { a.downloading = false }()

		for i, ep := range selected {
			filename := fmt.Sprintf("%03d - %s.mp3", ep.Index, sanitizeFilename(ep.Title))
			filePath := filepath.Join(outputDir, filename)

			fyne.Do(func() {
				a.statusLabel.SetText(fmt.Sprintf("Downloading %d/%d: %s", i+1, len(selected), ep.Title))
				a.progressBar.SetValue(0)
			})

			err := downloadFileWithProgress(filePath, ep.AudioURL, func(progress float64) {
				fyne.Do(func() {
					a.progressBar.SetValue(progress)
				})
			})

			if err != nil {
				fyne.Do(func() {
					a.statusLabel.SetText(fmt.Sprintf("Error: %v", err))
				})
				continue
			}

			// Add ID3 tags
			addID3Tags(filePath, ep, a.podcastInfo)
		}

		fyne.Do(func() {
			a.statusLabel.SetText(fmt.Sprintf("Downloaded %d episodes to %s", len(selected), outputDir))
			a.progressBar.SetValue(1)

			// Show completion dialog
			dialog.ShowInformation("Download Complete",
				fmt.Sprintf("Successfully downloaded %d episode(s) to:\n%s", len(selected), outputDir),
				a.mainWindow)

			a.showEpisodeView()
		})
	}()
}

func (a *App) getSelectedEpisodes() []Episode {
	var selected []Episode
	for _, ep := range a.episodes {
		if ep.Selected {
			selected = append(selected, ep)
		}
	}
	return selected
}

func (a *App) showError(title string, err error) {
	dialog.ShowError(fmt.Errorf("%s: %v", title, err), a.mainWindow)
	a.statusLabel.SetText("Error: " + err.Error())
}

// Core functions (reused from TUI)

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

func hasPodcastIndexCredentials() bool {
	apiKey := strings.TrimSpace(os.Getenv("PODCASTINDEX_API_KEY"))
	apiSecret := strings.TrimSpace(os.Getenv("PODCASTINDEX_API_SECRET"))
	return apiKey != "" && apiSecret != ""
}

func sanitizeFilename(name string) string {
	re := regexp.MustCompile(`[<>:"/\\|?*]`)
	name = re.ReplaceAllString(name, "")
	name = strings.TrimSpace(name)
	if len(name) > 100 {
		name = name[:100]
	}
	if name == "" {
		return "episode"
	}
	return name
}

func loadPodcastByID(podcastID string) (PodcastInfo, []Episode, error) {
	podcastID = strings.TrimPrefix(strings.ToLower(podcastID), "id")

	url := fmt.Sprintf("https://itunes.apple.com/lookup?id=%s&entity=podcast", podcastID)
	resp, err := http.Get(url)
	if err != nil {
		return PodcastInfo{}, nil, fmt.Errorf("failed to lookup podcast: %w", err)
	}
	defer resp.Body.Close()

	var result iTunesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return PodcastInfo{}, nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if result.ResultCount == 0 {
		return PodcastInfo{}, nil, fmt.Errorf("no podcast found with ID: %s", podcastID)
	}

	r := result.Results[0]
	info := PodcastInfo{
		Name:       r.CollectionName,
		Artist:     r.ArtistName,
		FeedURL:    r.FeedURL,
		ArtworkURL: r.ArtworkURL600,
		ID:         podcastID,
	}

	if info.ArtworkURL == "" {
		info.ArtworkURL = r.ArtworkURL100
	}

	if info.FeedURL == "" {
		return PodcastInfo{}, nil, fmt.Errorf("no RSS feed URL found for this podcast")
	}

	episodes, err := parseRSSFeed(info.FeedURL)
	if err != nil {
		return PodcastInfo{}, nil, err
	}

	return info, episodes, nil
}

func loadPodcastFromFeed(feedURL, name, artist, artworkURL string) (PodcastInfo, []Episode, error) {
	info := PodcastInfo{
		Name:       name,
		Artist:     artist,
		FeedURL:    feedURL,
		ArtworkURL: artworkURL,
	}

	fp := gofeed.NewParser()
	feed, err := fp.ParseURL(feedURL)
	if err != nil {
		return PodcastInfo{}, nil, fmt.Errorf("failed to parse RSS feed: %w", err)
	}

	if info.Name == "" && feed.Title != "" {
		info.Name = feed.Title
	}
	if info.Artist == "" && feed.Author != nil {
		info.Artist = feed.Author.Name
	}
	if info.ArtworkURL == "" && feed.Image != nil {
		info.ArtworkURL = feed.Image.URL
	}

	episodes, err := parseRSSFeedItems(feed.Items)
	if err != nil {
		return PodcastInfo{}, nil, err
	}

	return info, episodes, nil
}

func parseRSSFeed(feedURL string) ([]Episode, error) {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL(feedURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse RSS feed: %w", err)
	}
	return parseRSSFeedItems(feed.Items)
}

func parseRSSFeedItems(items []*gofeed.Item) ([]Episode, error) {
	var episodes []Episode
	for i, item := range items {
		audioURL := ""
		for _, enc := range item.Enclosures {
			if strings.Contains(enc.Type, "audio") || strings.HasSuffix(enc.URL, ".mp3") {
				audioURL = enc.URL
				break
			}
		}
		if audioURL == "" {
			continue
		}

		var pubDate time.Time
		if item.PublishedParsed != nil {
			pubDate = *item.PublishedParsed
		}

		duration := ""
		if item.ITunesExt != nil {
			duration = item.ITunesExt.Duration
		}

		episodes = append(episodes, Episode{
			Index:       i + 1,
			Title:       item.Title,
			Description: item.Description,
			AudioURL:    audioURL,
			PubDate:     pubDate,
			Duration:    duration,
		})
	}

	if len(episodes) == 0 {
		return nil, fmt.Errorf("no downloadable episodes found")
	}

	return episodes, nil
}

func searchAppleResults(query string) ([]SearchResult, error) {
	encodedQuery := strings.ReplaceAll(query, " ", "+")
	apiURL := fmt.Sprintf("https://itunes.apple.com/search?term=%s&media=podcast&limit=25", encodedQuery)

	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result iTunesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var results []SearchResult
	for _, r := range result.Results {
		if r.FeedURL == "" {
			continue
		}
		results = append(results, SearchResult{
			ID:         strconv.Itoa(r.CollectionID),
			Name:       r.CollectionName,
			Artist:     r.ArtistName,
			FeedURL:    r.FeedURL,
			ArtworkURL: r.ArtworkURL600,
			Source:     ProviderApple,
		})
	}
	return results, nil
}

func searchPodcastIndexResults(query string) ([]SearchResult, error) {
	apiKey := strings.TrimSpace(os.Getenv("PODCASTINDEX_API_KEY"))
	apiSecret := strings.TrimSpace(os.Getenv("PODCASTINDEX_API_SECRET"))

	apiHeaderTime := strconv.FormatInt(time.Now().Unix(), 10)
	hashInput := apiKey + apiSecret + apiHeaderTime
	h := sha1.New()
	h.Write([]byte(hashInput))
	authHash := hex.EncodeToString(h.Sum(nil))

	encodedQuery := url.QueryEscape(query)
	apiURL := fmt.Sprintf("https://api.podcastindex.org/api/1.0/search/byterm?q=%s&max=25", encodedQuery)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "PodcastDownload/1.0")
	req.Header.Set("X-Auth-Key", apiKey)
	req.Header.Set("X-Auth-Date", apiHeaderTime)
	req.Header.Set("Authorization", authHash)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	var result podcastIndexResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var results []SearchResult
	for _, feed := range result.Feeds {
		if feed.URL == "" {
			continue
		}
		results = append(results, SearchResult{
			ID:         strconv.Itoa(feed.ID),
			Name:       feed.Title,
			Artist:     feed.Author,
			FeedURL:    feed.URL,
			ArtworkURL: feed.Image,
			Source:     ProviderPodcastIndex,
		})
	}
	return results, nil
}

func searchBoth(query string) ([]SearchResult, error) {
	var wg sync.WaitGroup
	var appleResults, piResults []SearchResult
	var appleErr, piErr error

	wg.Add(2)

	go func() {
		defer wg.Done()
		appleResults, appleErr = searchAppleResults(query)
	}()

	go func() {
		defer wg.Done()
		piResults, piErr = searchPodcastIndexResults(query)
	}()

	wg.Wait()

	if appleErr != nil && piErr != nil {
		return nil, fmt.Errorf("search failed: Apple: %v, Podcast Index: %v", appleErr, piErr)
	}

	var combined []SearchResult
	seenFeedURLs := make(map[string]bool)

	if appleErr == nil {
		for _, r := range appleResults {
			normalizedURL := strings.ToLower(strings.TrimSuffix(r.FeedURL, "/"))
			if !seenFeedURLs[normalizedURL] {
				seenFeedURLs[normalizedURL] = true
				combined = append(combined, r)
			}
		}
	}
	if piErr == nil {
		for _, r := range piResults {
			normalizedURL := strings.ToLower(strings.TrimSuffix(r.FeedURL, "/"))
			if !seenFeedURLs[normalizedURL] {
				seenFeedURLs[normalizedURL] = true
				combined = append(combined, r)
			}
		}
	}

	return combined, nil
}

func downloadFileWithProgress(filepath string, fileURL string, progressCallback func(float64)) error {
	// Check if already exists
	if _, err := os.Stat(filepath); err == nil {
		progressCallback(1.0)
		return nil
	}

	resp, err := http.Get(fileURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

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
			if totalSize > 0 {
				percent := float64(downloaded) / float64(totalSize)
				if percent-lastPercent >= 0.01 || percent >= 1.0 {
					lastPercent = percent
					progressCallback(percent)
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

func addID3Tags(filepath string, ep Episode, info PodcastInfo) error {
	tag, err := id3v2.Open(filepath, id3v2.Options{Parse: true})
	if err != nil {
		tag = id3v2.NewEmptyTag()
	}
	defer tag.Close()

	tag.SetTitle(ep.Title)
	tag.SetArtist(info.Artist)
	tag.SetAlbum(info.Name)

	trackFrame := id3v2.TextFrame{
		Encoding: id3v2.EncodingUTF8,
		Text:     strconv.Itoa(ep.Index),
	}
	tag.AddFrame(tag.CommonID("Track number/Position in set"), trackFrame)

	return tag.Save()
}

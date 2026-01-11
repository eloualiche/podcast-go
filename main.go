package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
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

	"github.com/bogem/id3v2"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mmcdole/gofeed"
)

// Global program reference for sending messages from goroutines
var program *tea.Program

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			MarginBottom(1)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true)

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	checkboxStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			MarginTop(1)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("82")).
			Bold(true)
)

// PodcastInfo holds metadata from Apple's API
type PodcastInfo struct {
	Name       string
	Artist     string
	FeedURL    string
	ArtworkURL string
	ID         string
}

// SearchResult holds a podcast from search results
type SearchResult struct {
	ID         string
	Name       string
	Artist     string
	FeedURL    string
	ArtworkURL string
	Source     SearchProvider // which index this result came from
}

// Episode holds episode data from RSS feed
type Episode struct {
	Index       int
	Title       string
	Description string
	AudioURL    string
	PubDate     time.Time
	Duration    string
	Selected    bool
}

// iTunesResponse represents Apple's lookup API response
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

// podcastIndexResponse represents Podcast Index API search response
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

// SearchProvider indicates which podcast index to use
type SearchProvider string

const (
	ProviderApple        SearchProvider = "apple"
	ProviderPodcastIndex SearchProvider = "podcastindex"
)

// App states
type state int

const (
	stateLoading state = iota
	stateSearchResults
	statePreviewPodcast
	stateSelecting
	statePreviewEpisode
	stateDownloading
	stateDone
	stateError
)

// Model is our Bubble Tea model
type model struct {
	state          state
	podcastID      string
	searchQuery    string
	searchResults  []SearchResult
	podcastInfo    PodcastInfo
	episodes       []Episode
	cursor         int
	offset         int
	windowHeight   int
	spinner        spinner.Model
	progress       progress.Model
	loadingMsg     string
	errorMsg       string
	downloadIndex  int
	downloadTotal  int
	outputDir      string
	baseDir        string
	downloaded     []string
	percent        float64
	searchProvider SearchProvider
}

// Messages
type searchResultsMsg struct {
	results []SearchResult
}

type podcastLoadedMsg struct {
	info     PodcastInfo
	episodes []Episode
}

type errorMsg struct {
	err error
}

type downloadProgressMsg float64

type downloadCompleteMsg struct {
	filename string
}

type startDownloadMsg struct{}

type selectSearchResultMsg struct {
	result SearchResult
}

// isNumeric checks if a string is all digits (podcast ID)
func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

func initialModel(input string, baseDir string, provider SearchProvider) model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	p := progress.New(progress.WithDefaultGradient())

	isID := isNumeric(input)

	m := model{
		state:          stateLoading,
		spinner:        s,
		progress:       p,
		windowHeight:   24,
		baseDir:        baseDir,
		searchProvider: provider,
	}

	if isID {
		m.podcastID = input
		m.loadingMsg = "Looking up podcast..."
	} else {
		m.searchQuery = input
		var providerName string
		if provider == ProviderPodcastIndex {
			providerName = "Podcast Index"
		} else if hasPodcastIndexCredentials() {
			providerName = "Apple + Podcast Index"
		} else {
			providerName = "Apple Podcasts"
		}
		m.loadingMsg = fmt.Sprintf("Searching %s...", providerName)
	}

	return m
}

func (m model) Init() tea.Cmd {
	if m.searchQuery != "" {
		var searchCmd tea.Cmd
		// If credentials are available and no specific provider was forced, search both
		if hasPodcastIndexCredentials() && m.searchProvider == ProviderApple {
			searchCmd = searchBoth(m.searchQuery)
		} else if m.searchProvider == ProviderPodcastIndex {
			searchCmd = searchPodcastIndex(m.searchQuery)
		} else {
			searchCmd = searchPodcasts(m.searchQuery)
		}
		return tea.Batch(
			m.spinner.Tick,
			searchCmd,
		)
	}
	return tea.Batch(
		m.spinner.Tick,
		loadPodcast(m.podcastID),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.state {
		case stateSearchResults:
			return m.handleSearchResultsKeys(msg)
		case statePreviewPodcast:
			if msg.String() == "esc" || msg.String() == "b" || msg.String() == "v" {
				m.state = stateSearchResults
				return m, nil
			}
			if msg.String() == "ctrl+c" || msg.String() == "q" {
				return m, tea.Quit
			}
		case stateSelecting:
			return m.handleSelectionKeys(msg)
		case statePreviewEpisode:
			if msg.String() == "esc" || msg.String() == "b" || msg.String() == "v" {
				m.state = stateSelecting
				return m, nil
			}
			if msg.String() == "ctrl+c" || msg.String() == "q" {
				return m, tea.Quit
			}
		case stateDownloading:
			if msg.String() == "esc" || msg.String() == "b" {
				// Go back to episode selection
				m.state = stateSelecting
				m.downloadIndex = 0
				m.downloadTotal = 0
				m.percent = 0
				m.downloaded = nil
				return m, nil
			}
			if msg.String() == "ctrl+c" || msg.String() == "q" {
				return m, tea.Quit
			}
		case stateDone, stateError:
			if msg.String() == "q" || msg.String() == "ctrl+c" || msg.String() == "enter" {
				return m, tea.Quit
			}
		default:
			if msg.String() == "ctrl+c" || msg.String() == "q" {
				return m, tea.Quit
			}
		}

	case tea.WindowSizeMsg:
		m.windowHeight = msg.Height
		m.progress.Width = msg.Width - 10

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case searchResultsMsg:
		m.searchResults = msg.results
		if len(msg.results) == 0 {
			m.state = stateError
			m.errorMsg = fmt.Sprintf("No podcasts found for: %s", m.searchQuery)
			return m, nil
		}
		m.state = stateSearchResults
		m.cursor = 0
		m.offset = 0
		return m, nil

	case selectSearchResultMsg:
		m.state = stateLoading
		m.loadingMsg = fmt.Sprintf("Loading %s...", msg.result.Name)
		if msg.result.Source == ProviderPodcastIndex {
			// Load directly from RSS feed URL for Podcast Index results
			return m, loadPodcastFromFeed(msg.result.FeedURL, msg.result.Name, msg.result.Artist, msg.result.ArtworkURL)
		}
		m.podcastID = msg.result.ID
		return m, loadPodcast(msg.result.ID)

	case podcastLoadedMsg:
		m.state = stateSelecting
		m.podcastInfo = msg.info
		m.episodes = msg.episodes
		m.cursor = 0
		m.offset = 0
		return m, nil

	case errorMsg:
		m.state = stateError
		m.errorMsg = msg.err.Error()
		return m, nil

	case downloadProgressMsg:
		m.percent = float64(msg)
		cmd := m.progress.SetPercent(m.percent)
		return m, cmd

	case progress.FrameMsg:
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd

	case startDownloadMsg:
		return m, m.downloadNextCmd()

	case downloadCompleteMsg:
		m.downloaded = append(m.downloaded, msg.filename)
		m.downloadIndex++
		m.percent = 0
		if m.downloadIndex < m.downloadTotal {
			return m, m.downloadNextCmd()
		}
		m.state = stateDone
		return m, nil
	}

	return m, nil
}

func (m model) handleSearchResultsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visibleItems := m.windowHeight - 10
	if visibleItems < 5 {
		visibleItems = 5
	}

	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			if m.cursor < m.offset {
				m.offset = m.cursor
			}
		}

	case "down", "j":
		if m.cursor < len(m.searchResults)-1 {
			m.cursor++
			if m.cursor >= m.offset+visibleItems {
				m.offset = m.cursor - visibleItems + 1
			}
		}

	case "enter":
		if m.cursor < len(m.searchResults) {
			result := m.searchResults[m.cursor]
			return m, func() tea.Msg { return selectSearchResultMsg{result: result} }
		}

	case "v":
		if m.cursor < len(m.searchResults) {
			m.state = statePreviewPodcast
			return m, nil
		}
	}

	return m, nil
}

func (m model) handleSelectionKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visibleItems := m.windowHeight - 12
	if visibleItems < 5 {
		visibleItems = 5
	}

	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit

	case "esc", "b":
		// Go back to search results if available
		if len(m.searchResults) > 0 {
			m.state = stateSearchResults
			m.cursor = 0
			m.offset = 0
			return m, nil
		}
		// If no search results (direct podcast ID), quit
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			if m.cursor < m.offset {
				m.offset = m.cursor
			}
		}

	case "down", "j":
		if m.cursor < len(m.episodes)-1 {
			m.cursor++
			if m.cursor >= m.offset+visibleItems {
				m.offset = m.cursor - visibleItems + 1
			}
		}

	case "pgup":
		m.cursor -= visibleItems
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.offset = m.cursor

	case "pgdown":
		m.cursor += visibleItems
		if m.cursor >= len(m.episodes) {
			m.cursor = len(m.episodes) - 1
		}
		if m.cursor >= m.offset+visibleItems {
			m.offset = m.cursor - visibleItems + 1
		}

	case " ", "x":
		m.episodes[m.cursor].Selected = !m.episodes[m.cursor].Selected

	case "a":
		allSelected := true
		for _, ep := range m.episodes {
			if !ep.Selected {
				allSelected = false
				break
			}
		}
		for i := range m.episodes {
			m.episodes[i].Selected = !allSelected
		}

	case "enter":
		selected := m.getSelectedEpisodes()
		if len(selected) > 0 {
			m.state = stateDownloading
			m.downloadTotal = len(selected)
			m.downloadIndex = 0
			podcastFolder := sanitizeFilename(m.podcastInfo.Name)
			m.outputDir = filepath.Join(m.baseDir, podcastFolder)
			os.MkdirAll(m.outputDir, 0755)
			return m, func() tea.Msg { return startDownloadMsg{} }
		}

	case "v":
		if m.cursor < len(m.episodes) {
			m.state = statePreviewEpisode
			return m, nil
		}
	}

	return m, nil
}

func (m model) getSelectedEpisodes() []Episode {
	var selected []Episode
	for _, ep := range m.episodes {
		if ep.Selected {
			selected = append(selected, ep)
		}
	}
	return selected
}

func (m model) downloadNextCmd() tea.Cmd {
	selected := m.getSelectedEpisodes()
	if m.downloadIndex >= len(selected) {
		return nil
	}

	ep := selected[m.downloadIndex]
	currentFile := fmt.Sprintf("%03d - %s.mp3", ep.Index, sanitizeFilename(ep.Title))
	outputDir := m.outputDir
	podcastInfo := m.podcastInfo

	return func() tea.Msg {
		filePath := filepath.Join(outputDir, currentFile)

		// Download with progress callback that sends to program
		err := downloadFileWithProgress(filePath, ep.AudioURL)
		if err != nil {
			return errorMsg{err: err}
		}

		// Add ID3 tags
		addID3Tags(filePath, ep, podcastInfo)

		return downloadCompleteMsg{filename: filePath}
	}
}

func (m model) View() string {
	switch m.state {
	case stateLoading:
		return m.viewLoading()
	case stateSearchResults:
		return m.viewSearchResults()
	case statePreviewPodcast:
		return m.viewPreviewPodcast()
	case stateSelecting:
		return m.viewSelecting()
	case statePreviewEpisode:
		return m.viewPreviewEpisode()
	case stateDownloading:
		return m.viewDownloading()
	case stateDone:
		return m.viewDone()
	case stateError:
		return m.viewError()
	}
	return ""
}

func (m model) viewLoading() string {
	return fmt.Sprintf("\n  %s %s\n", m.spinner.View(), m.loadingMsg)
}

func (m model) viewSearchResults() string {
	var b strings.Builder

	// Header
	b.WriteString("\n")
	b.WriteString(titleStyle.Render(fmt.Sprintf("Search Results: \"%s\"", m.searchQuery)))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render(fmt.Sprintf("Found %d podcasts", len(m.searchResults))))
	b.WriteString("\n\n")

	// Calculate visible items
	visibleItems := m.windowHeight - 10
	if visibleItems < 5 {
		visibleItems = 5
	}

	// Results list
	end := m.offset + visibleItems
	if end > len(m.searchResults) {
		end = len(m.searchResults)
	}

	for i := m.offset; i < end; i++ {
		result := m.searchResults[i]
		cursor := "  "
		if i == m.cursor {
			cursor = "▸ "
		}

		// Truncate name
		name := result.Name
		if len(name) > 50 {
			name = name[:47] + "..."
		}

		// Truncate artist
		artist := result.Artist
		if len(artist) > 25 {
			artist = artist[:22] + "..."
		}

		line := fmt.Sprintf("%s%-50s  %s", cursor, name, dimStyle.Render(artist))

		if i == m.cursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	// Scroll indicator
	if len(m.searchResults) > visibleItems {
		b.WriteString(dimStyle.Render(fmt.Sprintf("\n  Showing %d-%d of %d", m.offset+1, end, len(m.searchResults))))
	}

	// Help
	b.WriteString(helpStyle.Render("\n\n  ↑/↓ navigate • enter select • v preview • q quit"))

	return b.String()
}

func (m model) viewPreviewPodcast() string {
	var b strings.Builder

	if m.cursor >= len(m.searchResults) {
		return ""
	}
	result := m.searchResults[m.cursor]

	b.WriteString("\n")
	b.WriteString(titleStyle.Render("Podcast Details"))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("  %s %s\n", subtitleStyle.Render("Name:"), result.Name))
	b.WriteString(fmt.Sprintf("  %s %s\n", subtitleStyle.Render("Artist:"), result.Artist))
	b.WriteString(fmt.Sprintf("  %s %s\n", subtitleStyle.Render("Source:"), string(result.Source)))
	if result.ID != "" {
		b.WriteString(fmt.Sprintf("  %s %s\n", subtitleStyle.Render("ID:"), result.ID))
	}
	if result.FeedURL != "" {
		b.WriteString(fmt.Sprintf("  %s %s\n", subtitleStyle.Render("Feed URL:"), result.FeedURL))
	}
	if result.ArtworkURL != "" {
		b.WriteString(fmt.Sprintf("  %s %s\n", subtitleStyle.Render("Artwork:"), result.ArtworkURL))
	}

	b.WriteString(helpStyle.Render("\n\n  esc/b/v back • q quit"))

	return b.String()
}

func (m model) viewSelecting() string {
	var b strings.Builder

	// Header
	b.WriteString("\n")
	b.WriteString(titleStyle.Render(m.podcastInfo.Name))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render(fmt.Sprintf("by %s • %d episodes", m.podcastInfo.Artist, len(m.episodes))))
	b.WriteString("\n\n")

	// Calculate visible items
	visibleItems := m.windowHeight - 12
	if visibleItems < 5 {
		visibleItems = 5
	}

	// Episode list
	end := m.offset + visibleItems
	if end > len(m.episodes) {
		end = len(m.episodes)
	}

	for i := m.offset; i < end; i++ {
		ep := m.episodes[i]
		cursor := "  "
		if i == m.cursor {
			cursor = "▸ "
		}

		checkbox := "○"
		if ep.Selected {
			checkbox = "●"
		}

		// Format date
		dateStr := ""
		if !ep.PubDate.IsZero() {
			dateStr = ep.PubDate.Format("2006-01-02")
		}

		// Truncate title
		title := ep.Title
		if len(title) > 45 {
			title = title[:42] + "..."
		}

		line := fmt.Sprintf("%s%s [%3d] %-45s %s  %s",
			cursor,
			checkboxStyle.Render(checkbox),
			ep.Index,
			title,
			dimStyle.Render(dateStr),
			dimStyle.Render(ep.Duration),
		)

		if i == m.cursor {
			b.WriteString(selectedStyle.Render(line))
		} else if ep.Selected {
			b.WriteString(normalStyle.Render(line))
		} else {
			b.WriteString(dimStyle.Render(line))
		}
		b.WriteString("\n")
	}

	// Scroll indicator
	if len(m.episodes) > visibleItems {
		b.WriteString(dimStyle.Render(fmt.Sprintf("\n  Showing %d-%d of %d", m.offset+1, end, len(m.episodes))))
	}

	// Selection count
	selectedCount := 0
	for _, ep := range m.episodes {
		if ep.Selected {
			selectedCount++
		}
	}
	b.WriteString(dimStyle.Render(fmt.Sprintf("  •  %d selected", selectedCount)))

	// Help
	b.WriteString(helpStyle.Render("\n\n  ↑/↓ navigate • space select • a toggle all • v preview • enter download • esc/b back • q quit"))

	return b.String()
}

func (m model) viewPreviewEpisode() string {
	var b strings.Builder

	if m.cursor >= len(m.episodes) {
		return ""
	}
	ep := m.episodes[m.cursor]

	b.WriteString("\n")
	b.WriteString(titleStyle.Render("Episode Details"))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("  %s %s\n", subtitleStyle.Render("Title:"), ep.Title))
	b.WriteString(fmt.Sprintf("  %s %d\n", subtitleStyle.Render("Episode #:"), ep.Index))
	if !ep.PubDate.IsZero() {
		b.WriteString(fmt.Sprintf("  %s %s\n", subtitleStyle.Render("Published:"), ep.PubDate.Format("January 2, 2006")))
	}
	if ep.Duration != "" {
		b.WriteString(fmt.Sprintf("  %s %s\n", subtitleStyle.Render("Duration:"), ep.Duration))
	}
	if ep.AudioURL != "" {
		b.WriteString(fmt.Sprintf("  %s %s\n", subtitleStyle.Render("Audio URL:"), ep.AudioURL))
	}

	// Description with word wrap
	if ep.Description != "" {
		b.WriteString(fmt.Sprintf("\n  %s\n", subtitleStyle.Render("Description:")))
		desc := ep.Description
		// Limit description length for display
		if len(desc) > 500 {
			desc = desc[:497] + "..."
		}
		// Simple word wrap at ~70 chars
		words := strings.Fields(desc)
		line := "  "
		for _, word := range words {
			if len(line)+len(word)+1 > 72 {
				b.WriteString(line + "\n")
				line = "  " + word
			} else {
				if line == "  " {
					line += word
				} else {
					line += " " + word
				}
			}
		}
		if line != "  " {
			b.WriteString(line + "\n")
		}
	}

	b.WriteString(helpStyle.Render("\n\n  esc/b/v back • q quit"))

	return b.String()
}

func (m model) viewDownloading() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(titleStyle.Render("Downloading..."))
	b.WriteString("\n\n")

	// Get current episode name
	currentFile := ""
	selected := m.getSelectedEpisodes()
	if m.downloadIndex < len(selected) {
		ep := selected[m.downloadIndex]
		currentFile = fmt.Sprintf("%03d - %s.mp3", ep.Index, sanitizeFilename(ep.Title))
	}

	b.WriteString(fmt.Sprintf("  Episode %d of %d\n", m.downloadIndex+1, m.downloadTotal))
	b.WriteString(fmt.Sprintf("  %s\n\n", currentFile))
	b.WriteString("  " + m.progress.View() + "\n")

	if len(m.downloaded) > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("\n  ✓ %d completed", len(m.downloaded))))
	}

	b.WriteString(helpStyle.Render("\n\n  esc/b back • q quit"))

	return b.String()
}

func (m model) viewDone() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(successStyle.Render("✓ Download Complete!"))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("  Downloaded %d episode(s) to:\n", len(m.downloaded)))
	b.WriteString(fmt.Sprintf("  %s/\n\n", m.outputDir))

	for _, f := range m.downloaded {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  • %s\n", filepath.Base(f))))
	}

	b.WriteString(helpStyle.Render("\n  Press enter or q to exit"))

	return b.String()
}

func (m model) viewError() string {
	return fmt.Sprintf("\n%s\n\n  %s\n\n%s",
		errorStyle.Render("Error"),
		m.errorMsg,
		helpStyle.Render("  Press q to exit"),
	)
}

// Fetch podcast info from Apple's API
func loadPodcast(podcastID string) tea.Cmd {
	return func() tea.Msg {
		// Remove "id" prefix if present
		podcastID = strings.TrimPrefix(strings.ToLower(podcastID), "id")

		// Fetch from iTunes API
		url := fmt.Sprintf("https://itunes.apple.com/lookup?id=%s&entity=podcast", podcastID)
		resp, err := http.Get(url)
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to lookup podcast: %w", err)}
		}
		defer resp.Body.Close()

		var result iTunesResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return errorMsg{err: fmt.Errorf("failed to parse response: %w", err)}
		}

		if result.ResultCount == 0 {
			return errorMsg{err: fmt.Errorf("no podcast found with ID: %s", podcastID)}
		}

		r := result.Results[0]
		info := PodcastInfo{
			Name:       r.CollectionName,
			Artist:     r.ArtistName,
			FeedURL:    r.FeedURL,
			ArtworkURL: r.ArtworkURL600,
		}

		if info.ArtworkURL == "" {
			info.ArtworkURL = r.ArtworkURL100
		}

		if info.FeedURL == "" {
			return errorMsg{err: fmt.Errorf("no RSS feed URL found for this podcast")}
		}

		// Parse RSS feed
		fp := gofeed.NewParser()
		feed, err := fp.ParseURL(info.FeedURL)
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to parse RSS feed: %w", err)}
		}

		var episodes []Episode
		for i, item := range feed.Items {
			audioURL := ""

			// Find audio enclosure
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
			return errorMsg{err: fmt.Errorf("no downloadable episodes found")}
		}

		return podcastLoadedMsg{info: info, episodes: episodes}
	}
}

func downloadFileWithProgress(filepath string, url string) error {
	// Check if already exists
	if _, err := os.Stat(filepath); err == nil {
		return nil
	}

	resp, err := http.Get(url)
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
				// Only send updates every 1% to avoid flooding
				if percent-lastPercent >= 0.01 || percent >= 1.0 {
					lastPercent = percent
					if program != nil {
						program.Send(downloadProgressMsg(percent))
					}
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
		// Create new tag if file doesn't have one
		tag = id3v2.NewEmptyTag()
	}
	defer tag.Close()

	tag.SetTitle(ep.Title)
	tag.SetArtist(info.Artist)
	tag.SetAlbum(info.Name)

	// Set track number
	trackFrame := id3v2.TextFrame{
		Encoding: id3v2.EncodingUTF8,
		Text:     strconv.Itoa(ep.Index),
	}
	tag.AddFrame(tag.CommonID("Track number/Position in set"), trackFrame)

	return tag.Save()
}

func sanitizeFilename(name string) string {
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

// searchPodcasts searches for podcasts using Apple's Search API
func searchPodcasts(query string) tea.Cmd {
	return func() tea.Msg {
		// URL encode the query
		encodedQuery := strings.ReplaceAll(query, " ", "+")
		url := fmt.Sprintf("https://itunes.apple.com/search?term=%s&media=podcast&limit=25", encodedQuery)

		resp, err := http.Get(url)
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to search podcasts: %w", err)}
		}
		defer resp.Body.Close()

		var result iTunesResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return errorMsg{err: fmt.Errorf("failed to parse search results: %w", err)}
		}

		var results []SearchResult
		for _, r := range result.Results {
			if r.FeedURL == "" {
				continue // Skip podcasts without RSS feed
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

		return searchResultsMsg{results: results}
	}
}

// searchPodcastIndex searches using Podcast Index API
func searchPodcastIndex(query string) tea.Cmd {
	return func() tea.Msg {
		apiKey := strings.TrimSpace(os.Getenv("PODCASTINDEX_API_KEY"))
		apiSecret := strings.TrimSpace(os.Getenv("PODCASTINDEX_API_SECRET"))

		if apiKey == "" || apiSecret == "" {
			return errorMsg{err: fmt.Errorf("Podcast Index API credentials not set.\nSet PODCASTINDEX_API_KEY and PODCASTINDEX_API_SECRET environment variables.\nGet free API keys at: https://api.podcastindex.org")}
		}

		// Build authentication headers (hash = sha1(apiKey + apiSecret + unixTime))
		apiHeaderTime := strconv.FormatInt(time.Now().Unix(), 10)
		hashInput := apiKey + apiSecret + apiHeaderTime
		h := sha1.New()
		h.Write([]byte(hashInput))
		authHash := hex.EncodeToString(h.Sum(nil))

		// URL encode the query
		encodedQuery := url.QueryEscape(query)
		apiURL := fmt.Sprintf("https://api.podcastindex.org/api/1.0/search/byterm?q=%s&max=25", encodedQuery)

		req, err := http.NewRequest("GET", apiURL, nil)
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to create request: %w", err)}
		}

		// Set required headers
		req.Header.Set("User-Agent", "PodcastDownload/1.0")
		req.Header.Set("X-Auth-Key", apiKey)
		req.Header.Set("X-Auth-Date", apiHeaderTime)
		req.Header.Set("Authorization", authHash)

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to search Podcast Index: %w", err)}
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return errorMsg{err: fmt.Errorf("Podcast Index API error (%d): %s", resp.StatusCode, string(body))}
		}

		var result podcastIndexResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return errorMsg{err: fmt.Errorf("failed to parse search results: %w", err)}
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

		return searchResultsMsg{results: results}
	}
}

// hasPodcastIndexCredentials checks if Podcast Index API credentials are set
func hasPodcastIndexCredentials() bool {
	apiKey := strings.TrimSpace(os.Getenv("PODCASTINDEX_API_KEY"))
	apiSecret := strings.TrimSpace(os.Getenv("PODCASTINDEX_API_SECRET"))
	return apiKey != "" && apiSecret != ""
}

// searchAppleResults performs Apple search and returns results directly (for use in combined search)
func searchAppleResults(query string) ([]SearchResult, error) {
	encodedQuery := strings.ReplaceAll(query, " ", "+")
	url := fmt.Sprintf("https://itunes.apple.com/search?term=%s&media=podcast&limit=25", encodedQuery)

	resp, err := http.Get(url)
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

// searchPodcastIndexResults performs Podcast Index search and returns results directly (for use in combined search)
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

// searchBoth searches both Apple and Podcast Index APIs concurrently and combines results
func searchBoth(query string) tea.Cmd {
	return func() tea.Msg {
		var wg sync.WaitGroup
		var appleResults, piResults []SearchResult
		var appleErr, piErr error

		wg.Add(2)

		// Search Apple
		go func() {
			defer wg.Done()
			appleResults, appleErr = searchAppleResults(query)
		}()

		// Search Podcast Index
		go func() {
			defer wg.Done()
			piResults, piErr = searchPodcastIndexResults(query)
		}()

		wg.Wait()

		// If both failed, return error
		if appleErr != nil && piErr != nil {
			return errorMsg{err: fmt.Errorf("search failed: Apple: %v, Podcast Index: %v", appleErr, piErr)}
		}

		// Combine results - Apple first, then Podcast Index (deduplicated by feed URL)
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

		return searchResultsMsg{results: combined}
	}
}

// loadPodcastFromFeed loads a podcast directly from its RSS feed URL
func loadPodcastFromFeed(feedURL, name, artist, artworkURL string) tea.Cmd {
	return func() tea.Msg {
		info := PodcastInfo{
			Name:       name,
			Artist:     artist,
			FeedURL:    feedURL,
			ArtworkURL: artworkURL,
		}

		// Parse RSS feed
		fp := gofeed.NewParser()
		feed, err := fp.ParseURL(feedURL)
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to parse RSS feed: %w", err)}
		}

		// Use feed title/author if not provided
		if info.Name == "" && feed.Title != "" {
			info.Name = feed.Title
		}
		if info.Artist == "" && feed.Author != nil {
			info.Artist = feed.Author.Name
		}
		if info.ArtworkURL == "" && feed.Image != nil {
			info.ArtworkURL = feed.Image.URL
		}

		var episodes []Episode
		for i, item := range feed.Items {
			audioURL := ""

			// Find audio enclosure
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
			return errorMsg{err: fmt.Errorf("no downloadable episodes found")}
		}

		return podcastLoadedMsg{info: info, episodes: episodes}
	}
}

func main() {
	// Define flags
	baseDir := flag.String("o", ".", "Base directory where the podcast folder will be created")
	indexFlag := flag.String("index", "apple", "Search provider: 'apple' (default) or 'podcastindex'")

	// Custom usage message
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <podcast_id_or_search_query>\n\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "Flags:")
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr, "\nExamples:")
		fmt.Fprintln(os.Stderr, "  podcastdownload -o ~/Music \"the daily\"")
		fmt.Fprintln(os.Stderr, "  podcastdownload 1200361736")
		fmt.Fprintln(os.Stderr, "  podcastdownload --index podcastindex \"france inter\"")
		fmt.Fprintln(os.Stderr, "\nPodcast Index:")
		fmt.Fprintln(os.Stderr, "  To use Podcast Index, set these environment variables:")
		fmt.Fprintln(os.Stderr, "    PODCASTINDEX_API_KEY=your_key")
		fmt.Fprintln(os.Stderr, "    PODCASTINDEX_API_SECRET=your_secret")
		fmt.Fprintln(os.Stderr, "  Get free API keys at: https://api.podcastindex.org")
	}

	flag.Parse()

	// Parse the index flag
	var provider SearchProvider
	switch strings.ToLower(*indexFlag) {
	case "podcastindex", "pi":
		provider = ProviderPodcastIndex
	default:
		provider = ProviderApple
	}

	// Check if we have arguments left after parsing flags (the search query)
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	// Join remaining arguments to form the search query
	input := strings.Join(flag.Args(), " ")

	// Pass the baseDir and provider to initialModel
	program = tea.NewProgram(initialModel(input, *baseDir, provider), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
# Podcast Downloader

A TUI (Terminal User Interface) application for downloading podcast episodes from Apple Podcasts.

## Features

- Search podcasts by name or lookup by Apple Podcast ID
- Interactive episode selection with keyboard navigation
- Progress bar for downloads
- Automatic ID3v2 tag writing (title, artist, album, track number)
- Colorful terminal interface using Bubble Tea

## Installation

```bash
go build -o podcastdownload main.go
```

Or with a local GOPATH:

```bash
GOPATH=$(pwd)/.go go build -o podcastdownload main.go
```

## Usage

```bash
# Search by podcast name
./podcastdownload "the daily"
./podcastdownload "new york times"

# Lookup by Apple Podcast ID
./podcastdownload 1200361736
```

Find the podcast ID in the Apple Podcasts URL:
```
https://podcasts.apple.com/us/podcast/the-daily/id1200361736
                                              ^^^^^^^^^^
```

## Controls

### Search Results Screen
- `↑/↓` or `k/j` - Navigate results
- `Enter` - Select podcast
- `q` - Quit

### Episode Selection Screen
- `↑/↓` or `k/j` - Navigate episodes
- `Space` or `x` - Toggle selection
- `a` - Select/deselect all
- `PgUp/PgDn` - Page navigation
- `Enter` - Start download
- `q` - Quit

## Dependencies

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) - Styling
- [Bubbles](https://github.com/charmbracelet/bubbles) - TUI components
- [gofeed](https://github.com/mmcdole/gofeed) - RSS parsing
- [id3v2](https://github.com/bogem/id3v2) - ID3 tag writing

## License

MIT

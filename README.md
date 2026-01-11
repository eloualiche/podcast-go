<div align="center">

# Podcast Downloader
### Vibe coded Go TUI for downloading your favorites

<img src="./media/demo-podcastdownload.gif" width="800" alt="Demo of the TUI">

</div>

---

## Features

- **Search by name**: Search for any podcast by name using Apple's podcast directory
- **Lookup by ID**: Direct lookup using Apple Podcast ID for faster access
- **Interactive selection**: Browse and select specific episodes to download
- **Batch downloads**: Select multiple episodes at once with visual progress tracking
- **ID3 tagging**: Automatically writes ID3v2 tags (title, artist, album, track number)
- **Smart file naming**: Episodes are saved with track numbers for proper ordering
- **Resume support**: Skips already downloaded files

## Requirements

- Go 1.21 or later
- [just](https://github.com/casey/just) (optional, for build commands)

## Installation

### Clone from GitHub

```bash
git clone https://github.com/eloualiche/podcast-go.git
cd podcast-go
```

### Build

Using just (recommended):

```bash
just build
```

Or using go directly:

```bash
go build -o podcastdownload main.go
```

### Install globally (optional)

To use `podcastdownload` from anywhere:

```bash
go install
```

Or move the binary to your PATH:

```bash
sudo mv podcastdownload /usr/local/bin/
```

## Usage

### Basic Commands

```bash
# Search for a podcast by name
./podcastdownload "the daily"

# Search with multiple words
./podcastdownload "new york times podcast"

# Lookup by Apple Podcast ID (faster, no search step)
./podcastdownload 1200361736
```

### Finding a Podcast ID

The podcast ID can be found in any Apple Podcasts URL:

```
https://podcasts.apple.com/us/podcast/the-daily/id1200361736
                                                  ^^^^^^^^^^
                                                  This is the ID
```

You can also:
1. Open Apple Podcasts app or website
2. Navigate to the podcast page
3. Copy the URL - the ID is the number after `id`

## Workflow

### 1. Search or Lookup

When you run the app with a search query, you'll see matching podcasts:

```
Search Results: "the daily"
Found 25 podcasts

▸ The Daily                                         The New York Times
  The Daily Beans                                   MSW Media
  Daily Tech News Show                              Tom Merritt
  ...
```

### 2. Select Episodes

After choosing a podcast, browse the episode list:

```
The Daily
by The New York Times • 2847 episodes

▸ ○ [  1] The Sunday Read: 'The Kidnapping...       2024-01-07  45:32
  ● [  2] A Landmark satisfies Lawsuit...           2024-01-06  28:15
  ● [  3] The Fight Over the Future...              2024-01-05  31:42
  ...

  Showing 1-20 of 2847  •  2 selected

  ↑/↓ navigate • space select • a toggle all • enter download • q quit
```

### 3. Download

Selected episodes are downloaded with a progress bar:

```
Downloading...

  Episode 1 of 2
  002 - A Landmark Lawsuit.mp3

  ████████████████████░░░░░░░░░░░░░░░░░░░░ 52%

  ✓ 0 completed
```

### 4. Output

Episodes are saved to a folder named after the podcast:

```
The Daily/
├── 001 - The Sunday Read.mp3
├── 002 - A Landmark Lawsuit.mp3
└── 003 - The Fight Over the Future.mp3
```

Each file includes ID3 tags:
- **Title**: Episode title
- **Artist**: Podcast creator/network
- **Album**: Podcast name
- **Track**: Episode number

## Keyboard Controls

### Search Results Screen

| Key | Action |
|-----|--------|
| `↑` / `k` | Move cursor up |
| `↓` / `j` | Move cursor down |
| `Enter` | Select podcast |
| `q` / `Ctrl+C` | Quit |

### Episode Selection Screen

| Key | Action |
|-----|--------|
| `↑` / `k` | Move cursor up |
| `↓` / `j` | Move cursor down |
| `Space` / `x` | Toggle episode selection |
| `a` | Select/deselect all episodes |
| `PgUp` | Page up |
| `PgDn` | Page down |
| `Enter` | Start downloading selected |
| `q` / `Ctrl+C` | Quit |

### Download/Complete Screen

| Key | Action |
|-----|--------|
| `Enter` / `q` | Exit (when complete) |
| `Ctrl+C` | Cancel download |

## Build Commands

Using `just`:

```bash
just build    # Build the binary
just run      # Build and run
just clean    # Remove build artifacts
```

Or use `just --list` to see all available commands.

## Dependencies

| Library | Purpose |
|---------|---------|
| [Bubble Tea](https://github.com/charmbracelet/bubbletea) | TUI framework |
| [Lip Gloss](https://github.com/charmbracelet/lipgloss) | Terminal styling |
| [Bubbles](https://github.com/charmbracelet/bubbles) | Progress bar, spinner components |
| [gofeed](https://github.com/mmcdole/gofeed) | RSS/Atom feed parsing |
| [id3v2](https://github.com/bogem/id3v2) | MP3 ID3 tag writing |

## How It Works

1. **Search/Lookup**: Uses Apple's iTunes Search API to find podcasts
2. **Feed Parsing**: Fetches and parses the podcast's RSS feed using gofeed
3. **Download**: Downloads MP3 files from the enclosure URLs in the RSS feed
4. **Tagging**: Writes ID3v2 tags to each downloaded file

## Troubleshooting

### "No RSS feed URL found"

Some podcasts don't expose their RSS feed publicly. This app requires access to the RSS feed to download episodes.

### "No downloadable episodes found"

The podcast's RSS feed doesn't contain audio enclosures, or uses a format not recognized as audio.

### Download seems stuck

Some podcast CDNs may be slow. The progress bar updates every 1% of download progress. For large files on slow connections, this may take a moment.

## License

MIT

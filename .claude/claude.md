# Podcast Downloader

A TUI (Terminal User Interface) application for downloading podcast episodes.

## Project Overview

This is a Go-based podcast downloader with an interactive terminal interface built using the Bubble Tea framework. The user **does not know Go** - Claude should explain Go concepts when relevant and avoid assuming Go knowledge.

## Quick Reference

```bash
# Build
go build -o podcastdownload .

# Run tests
go test ./...

# Run the app
./podcastdownload "podcast name"        # Search by name
./podcastdownload 1200361736            # By Apple Podcasts ID
./podcastdownload -o ~/Music "the daily"  # Specify output directory
./podcastdownload --index podcastindex "france inter"  # Use Podcast Index API
```

## Codebase Structure

```
.
├── main.go                 # Main application with TUI logic
├── internal/
│   └── podcast/
│       ├── utils.go        # Reusable utilities (download, sanitize, etc.)
│       └── utils_test.go   # Unit tests
├── go.mod                  # Go module definition
├── go.sum                  # Dependency checksums
└── .github/
    └── workflows/
        └── go.yml          # CI workflow (build + test)
```

## Key Features

- **Search**: Search podcasts via Apple Podcasts or Podcast Index APIs
- **Podcast Index**: Set `PODCASTINDEX_API_KEY` and `PODCASTINDEX_API_SECRET` env vars
- **Preview**: Press `v` to preview podcast/episode details
- **Navigation**: Arrow keys, `j/k` for vim-style, `pgup/pgdown`
- **Selection**: `space` to toggle, `a` to select all
- **Download**: `enter` to download selected episodes with ID3 tags

## Known Issues & Fixes

### Acast Downloads (e.g., Slate Political Gabfest)
Acast blocks Go's default User-Agent and uses non-standard text-based redirects. The fix is in `internal/podcast/utils.go`:
- Custom User-Agent: `PodcastDownloader/1.0`
- Handle `"Redirecting to <url>"` text responses
- Decode HTML entities (`&amp;` → `&`)

## CI/CD

- GitHub Actions runs on PRs and pushes to `main`
- Runs: `go build`, `go test -race`
- Linting is currently disabled (golangci-lint doesn't support Go 1.25 yet)

## Development Notes

- The `internal/` directory is a Go convention for packages that shouldn't be imported by external code
- Tests use `httptest` to mock HTTP servers
- The TUI uses Bubble Tea's Elm-like architecture (Model, Update, View)

## Related Repos

There's another repo at `../PodcastDownload` with the Go code in a `go/` subdirectory. This repo (PodcastDownloadApp) is the canonical version with more features.

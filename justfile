# Build the TUI podcast downloader
build:
    go build -o podcastdownload main.go

# Build the GUI podcast downloader
build-gui:
    go build -o podcast-gui ./cmd/gui/

# Build both versions
build-all: build build-gui

# Remove build artifacts
clean:
    rm -f podcastdownload podcast-gui

# Build and run the TUI application
run: build
    ./podcastdownload

# Build and run the GUI application
run-gui: build-gui
    ./podcast-gui

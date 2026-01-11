# Build the podcast downloader
build:
    go build -o podcastdownload main.go

# Remove build artifacts
clean:
    rm -f podcastdownload

# Build and run the application
run: build
    ./podcastdownload

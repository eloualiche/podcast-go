.PHONY: build clean run

build:
	go build -o podcastdownload main.go

clean:
	rm -f podcastdownload

run: build
	./podcastdownload

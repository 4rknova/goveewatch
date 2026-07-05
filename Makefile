all: goveewatch

goveewatch: main.go go.sum
	go build -o goveewatch .

install: goveewatch
	install -m 755 goveewatch /usr/bin/goveewatch

uninstall:
	rm -f /usr/bin/goveewatch

deps:
	go mod download

clean:
	rm -f goveewatch

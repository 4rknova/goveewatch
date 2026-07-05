all: goveewatch

goveewatch: main.go go.sum
	go build -o goveewatch .

install: goveewatch
	install goveewatch /usr/bin/

deps:
	go mod download

clean:
	rm -f goveewatch

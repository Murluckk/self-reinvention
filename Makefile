BINARY := tracker-bot

.PHONY: build test vet fmt run clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BINARY) ./cmd/bot

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY)

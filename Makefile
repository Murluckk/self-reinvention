BINARY := tracker-bot
VERSION := $(shell git describe --always --dirty 2>/dev/null || echo dev)

# Локальные настройки выкатки (DEPLOY_HOST=root@…), в git не попадают.
-include .deploy.env

.PHONY: build test vet fmt run clean deploy

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o $(BINARY) ./cmd/bot

test:
	go test ./...

# Выкатка на VPS с копией базы и автоматическим откатом: deploy/deploy.sh.
deploy:
	DEPLOY_HOST="$(DEPLOY_HOST)" ./deploy/deploy.sh

vet:
	go vet ./...

fmt:
	gofmt -l -w .

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY)

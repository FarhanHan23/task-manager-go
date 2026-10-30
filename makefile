APP     := todo-api
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: run build test cover lint fmt clean docker

run:
	go run ./cmd/api

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(APP) ./cmd/api

test:
	go test -race -count=1 ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

lint:
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...

fmt:
	gofmt -w .

clean:
	rm -rf bin coverage.out

docker:
	docker build --build-arg VERSION=$(VERSION) -t $(APP):$(VERSION) .

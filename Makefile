.PHONY: build install test fmt vet tidy lint update-mermaid release-snapshot clean

MERMAID_VERSION ?= 11

build:
	go build -o ./bin/mdview ./cmd/mdview

install:
	go install ./cmd/mdview

test:
	go test ./...

fmt:
	gofmt -s -w .

vet:
	go vet ./...

tidy:
	go mod tidy

lint:
	golangci-lint run

update-mermaid:
	curl -fsSL "https://cdn.jsdelivr.net/npm/mermaid@$(MERMAID_VERSION)/dist/mermaid.min.js" \
		-o internal/render/assets/mermaid.min.js
	@wc -c internal/render/assets/mermaid.min.js

release-snapshot:
	goreleaser release --clean --snapshot --skip=publish

clean:
	rm -rf ./bin ./dist

.DEFAULT_GOAL := help

.PHONY: help format lint vet test fuzz ci build run

help: ## Print each target with a one-line description
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "%-8s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

format: ## Rewrite Go files with gofmt
	gofmt -w .

lint: ## Fail on gofmt or golangci-lint findings
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then printf '%s\n' "$$out"; exit 1; fi
	"$$(go env GOPATH)/bin/golangci-lint" run

vet: ## Run go vet ./...
	go vet ./...

test: ## Run go test -race ./...
	go test -race ./...

fuzz: ## Fuzz path mutation and XML round trips
	go test ./internal/script -run '^$$' -fuzz FuzzSetPath -fuzztime=10s -parallel=2
	go test ./internal/xml -run '^$$' -fuzz FuzzDocument -fuzztime=10s -parallel=2

ci: lint vet test fuzz build ## Run formatting, lint, vet, race, fuzz, and native build checks

build: ## Build the no-CGO CLI to bin/gi
	mkdir -p bin
	CGO_ENABLED=0 go build -o bin/gi ./cmd/gi

run: ## Run the gi CLI, forwarding extra arguments
	go run ./cmd/gi $(ARGS)

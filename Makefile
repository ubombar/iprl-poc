# Makefile
.PHONY: proto build clean test lint run-orchestrator run-agent run-generator install-tools help

# Directories
PROTO_DIR := proto
GEN_DIR := internal/gen/proto
BIN_DIR := bin

# Binary names
BINARIES := generator orchestrator agent

# Go agentrameters
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOMOD := $(GOCMD) mod

# Build flags
LDFLAGS := -ldflags "-s -w"
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT := $(git rev-parse --short HEAD)
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS_VERSION := -ldflags "-s -w -X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildTime=$(BUILD_TIME)"

# Default target
.DEFAULT_GOAL := help

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':' | sed 's/^/  /'

## proto: Generate protobuf code
proto:
	@mkdir -p $(GEN_DIR)
	protoc \
		--go_out=$(GEN_DIR) --go_opt=paths=source_relative \
		--go-grpc_out=$(GEN_DIR) --go-grpc_opt=paths=source_relative \
		-I$(PROTO_DIR) \
		$(PROTO_DIR)/*.proto

## install-tools: Install development tools
install-tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

## build: Build all binaries
build: proto
	@mkdir -p $(BIN_DIR)
	$(GOBUILD) $(LDFLAGS_VERSION) -o $(BIN_DIR)/generator ./cmd/generator
	$(GOBUILD) $(LDFLAGS_VERSION) -o $(BIN_DIR)/orchestrator ./cmd/orchestrator
	$(GOBUILD) $(LDFLAGS_VERSION) -o $(BIN_DIR)/agent ./cmd/agent

## build-generator: Build only generator
build-generator: proto
	@mkdir -p $(BIN_DIR)
	$(GOBUILD) $(LDFLAGS_VERSION) -o $(BIN_DIR)/generator ./cmd/generator

## build-orchestrator: Build only orchestrator
build-orchestrator: proto
	@mkdir -p $(BIN_DIR)
	$(GOBUILD) $(LDFLAGS_VERSION) -o $(BIN_DIR)/orchestrator ./cmd/orchestrator

## build-agent: Build only agent
build-agent: proto
	@mkdir -p $(BIN_DIR)
	$(GOBUILD) $(LDFLAGS_VERSION) -o $(BIN_DIR)/agent ./cmd/agent

## clean: Remove build artifacts
clean:
	rm -rf $(BIN_DIR)/
	rm -rf $(GEN_DIR)/*.pb.go

## test: Run tests
test:
	$(GOTEST) -v -race ./...

## test-coverage: Run tests with coverage
test-coverage:
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

## lint: Run linter
lint:
	golangci-lint run ./...

## tidy: Tidy go modules
tidy:
	$(GOMOD) tidy

## deps: Download dependencies
deps:
	$(GOMOD) download

## run-orchestrator: Run the Probing Orchestrator
run-orchestrator: build-orchestrator
	./$(BIN_DIR)/orchestrator --address=:50050

## run-agent: Run a Probing Agent
run-agent: build-agent
	./$(BIN_DIR)/agent --address=:50051 --orchestrator-addr=localhost:50050

## run-generator: Run a Probing Directive Generator
run-generator: build-generator
	./$(BIN_DIR)/generator --address=:50049 --orchestrator-addr=localhost:50050

## run-all: Run all comorchestratornents (requires tmux)
run-all: build
	@echo "Starting all comorchestratornents..."
	@tmux new-session -d -s iprl './$(BIN_DIR)/orchestrator --address=:50050'
	@sleep 1
	@tmux split-window -h './$(BIN_DIR)/agent --address=:50051 --orchestrator-addr=localhost:50050'
	@tmux split-window -v './$(BIN_DIR)/generator --address=:50049 --orchestrator-addr=localhost:50050'
	@tmux attach-session -t iprl

## docker-build: Build Docker images
docker-build:
	docker build -t iprl-orchestrator:$(VERSION) -f docker/Dockerfile.orchestrator .
	docker build -t iprl-agent:$(VERSION) -f docker/Dockerfile.agent .
	docker build -t iprl-generator:$(VERSION) -f docker/Dockerfile.generator .

## version: Show version
version:
	@echo $(VERSION)

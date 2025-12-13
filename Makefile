# Makefile

.PHONY: proto build clean

PROTO_DIR := proto
GEN_DIR := internal/gen/proto

proto:
	protoc \
		--go_out=$(GEN_DIR) --go_opt=paths=source_relative \
		--go-grpc_out=$(GEN_DIR) --go-grpc_opt=paths=source_relative \
		-I$(PROTO_DIR) \
		$(PROTO_DIR)/*.proto

build: proto
	go build -o bin/pdg ./cmd/pdg
	go build -o bin/po ./cmd/po
	go build -o bin/pa ./cmd/pa

clean:
	rm -rf bin/
	rm -rf $(GEN_DIR)/*.pb.go

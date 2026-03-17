.PHONY: dev dev-be dev-fe build-fe build build-homeserver dev-homeserver

DIR ?= .

dev-be:
	go run ./cmd/server/main.go $(DIR)

dev-fe:
	cd frontend && npm run dev

dev-homeserver:
	go run ./cmd/homeserver/main.go

build-fe:
	cd frontend && npm run build

build: build-fe
	go build -o bin/coding-tutor ./cmd/server/main.go

build-homeserver: build-fe
	go build -o bin/coding-tutor-server ./cmd/homeserver/main.go

dev:
	@echo "Starting local server on :47291 and frontend on :5173"
	@trap 'kill %1 %2 2>/dev/null; exit' INT; \
	go run ./cmd/server/main.go $(DIR) & \
	cd frontend && npm run dev & \
	wait

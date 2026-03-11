.PHONY: dev dev-be dev-fe build-fe build

dev-be:
	go run ./cmd/server/main.go

dev-fe:
	cd frontend && BACKEND_PORT=$${BACKEND_PORT:-8080} npm run dev

build-fe:
	cd frontend && npm run build

build: build-fe
	go build -o bin/coding-tutor ./cmd/server/main.go

dev:
	@echo "Starting backend on :8080 and frontend on :5173"
	@trap 'kill %1 %2 2>/dev/null; exit' INT; \
	go run ./cmd/server/main.go & \
	cd frontend && npm run dev & \
	wait

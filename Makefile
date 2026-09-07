.PHONY: dev dev-be dev-fe test lint build-fe build-be build

DIR ?= $(CURDIR)/data

dev-be:
	CLINIC_SITE_URL=http://localhost:5173 go run ./cmd/clinic "$(DIR)"

dev-fe:
	cd frontend && npm run dev

build-fe:
	cd frontend && npm run build

build-be:
	mkdir -p bin
	go build -o bin/clinic ./cmd/clinic

build: build-fe build-be

test:
	go test ./cmd/... ./internal/...
	npm --prefix frontend test

lint:
	go vet ./cmd/... ./internal/...
	cd frontend && npm run lint

dev:
	@echo "Starting local server on :47291 and frontend on :5173"
	@(cd frontend && npm run dev) & frontend_pid=$$!; \
	trap 'kill $$frontend_pid 2>/dev/null; wait $$frontend_pid 2>/dev/null' EXIT INT TERM; \
	curl --silent --show-error --fail --retry 10 --retry-delay 1 --retry-connrefused --max-time 2 http://localhost:5173/clinic-config.json >/dev/null || exit 1; \
	CLINIC_SITE_URL=http://localhost:5173 go run ./cmd/clinic "$(DIR)" & clinic_pid=$$!; \
	trap 'kill $$clinic_pid $$frontend_pid 2>/dev/null; wait $$clinic_pid $$frontend_pid 2>/dev/null' EXIT INT TERM; \
	wait $$clinic_pid $$frontend_pid

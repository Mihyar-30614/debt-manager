.PHONY: run dev install-air help

help:
	@echo "Available commands:"
	@echo "  make run      - Run the server normally"
	@echo "  make dev      - Run with hot reload (requires air)"
	@echo "  make install-air - Install air for hot reload"

run:
	go run .

dev:
	@AIR_BIN=$$(command -v air 2>/dev/null); \
	if [ -z "$$AIR_BIN" ]; then \
		AIR_BIN="$(shell go env GOPATH)/bin/air"; \
	fi; \
	if [ -f "$$AIR_BIN" ]; then \
		$$AIR_BIN; \
	else \
		echo "Error: 'air' is not installed or not found."; \
		echo "Install it with: make install-air"; \
		echo "Then ensure $(shell go env GOPATH)/bin is in your PATH."; \
		echo "Or run directly: $(shell go env GOPATH)/bin/air"; \
		exit 1; \
	fi

install-air:
	@echo "Installing air..."
	@go install github.com/cosmtrek/air@v1.49.0
	@echo "Air installed! Make sure $(shell go env GOPATH)/bin is in your PATH."
	@echo "Run 'make dev' to start hot reload."

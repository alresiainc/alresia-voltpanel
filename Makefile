APP=voltpanel
DIST=dist
UI_DIR=ui
CMD_DIR=cmd/voltpanel
BIN=$(DIST)/$(APP)

.PHONY: all build ui-build ui-dev clean dev test release-snapshot

all: build

build: ui-build
	@echo "Building $(APP) with embedded UI..."
	@mkdir -p $(DIST)
	GO111MODULE=on CGO_ENABLED=0 go build -o $(BIN) ./cmd/voltpanel
	@echo "Built $(BIN)"

ui-build:
	@echo "Building UI..."
	@cd $(UI_DIR) && pnpm install && pnpm build
	@mkdir -p $(CMD_DIR)/dist && cp -R $(UI_DIR)/dist/* $(CMD_DIR)/dist/

ui-dev:
	@cd $(UI_DIR) && pnpm install && pnpm dev

clean:
	rm -rf $(DIST)
	rm -rf $(CMD_DIR)/dist
	cd $(UI_DIR) && rm -rf dist node_modules

# Run backend with embedded UI if built; otherwise just backend API
# Set DEV=1 to relax auth for local dev
DEV?=0
PORT?=7788

dev:
	DEV=$(DEV) PORT=$(PORT) go run ./cmd/voltpanel

release-snapshot:
	goreleaser release --clean --snapshot

 test:
	go test ./...

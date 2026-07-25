# PlusClouds Ubuntu Agent Makefile

BINARY_AGENT   := plusclouds-agent
BINARY_CTL     := plsctl
BINARY_LINUX   := plusclouds.linux
BINARY_WINDOWS := plusclouds.windows
BINARY_FREEBSD       := plusclouds.freebsd
BINARY_FREEBSD_ARM64 := plusclouds.freebsd-arm64
CMD_AGENT      := ./cmd/agent
CMD_CTL        := ./cmd/ctl
BUILD_DIR      := ./bin

VERSION        ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT         ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Development build — fast, includes debug info
LDFLAGS        := -ldflags "-X github.com/plusclouds/ubuntu-agent/internal/config.AgentVersion=$(VERSION)"

# Production build — static binary, stripped, no debug info
LDFLAGS_PROD   := -ldflags "-s -w -X github.com/plusclouds/ubuntu-agent/internal/config.AgentVersion=$(VERSION)"

INSTALL_DIR    := /usr/local/bin
SERVICE_DIR    := /etc/systemd/system
CONFIG_DIR     := /etc/plusclouds
LOG_DIR        := /var/log/plusclouds
ISO_MOUNT_DIR  := /media/plusclouds-config

.PHONY: all build build-agent build-ctl build-prod build-linux build-windows build-freebsd build-freebsd-arm64 build-all test lint clean install uninstall package-deb help

## all: build both binaries (development)
all: build

## build: build both binaries for development (current OS/arch)
build: build-agent build-ctl

## build-agent: build the agent daemon (development)
build-agent:
	@echo "Building $(BINARY_AGENT) (dev)..."
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_AGENT) $(CMD_AGENT)
	@echo "Done: $(BUILD_DIR)/$(BINARY_AGENT)"

## build-ctl: build the plsctl CLI (development)
build-ctl:
	@echo "Building $(BINARY_CTL) (dev)..."
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_CTL) $(CMD_CTL)
	@echo "Done: $(BUILD_DIR)/$(BINARY_CTL)"

## build-prod: alias for build-linux (backwards compatibility)
build-prod: build-linux

## build-linux: build production binary for Linux amd64 → bin/plusclouds.linux
build-linux:
	@echo "Building $(BINARY_LINUX) $(VERSION) (linux/amd64)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -trimpath $(LDFLAGS_PROD) \
		-o $(BUILD_DIR)/$(BINARY_LINUX) $(CMD_AGENT)
	@echo "Done: $(BUILD_DIR)/$(BINARY_LINUX)"
	@echo "Size: $$(du -sh $(BUILD_DIR)/$(BINARY_LINUX) | cut -f1)"
	@file $(BUILD_DIR)/$(BINARY_LINUX)

## build-windows: build production binary for Windows amd64 → bin/plusclouds.windows
build-windows:
	@echo "Building $(BINARY_WINDOWS) $(VERSION) (windows/amd64)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
		go build -trimpath $(LDFLAGS_PROD) \
		-o $(BUILD_DIR)/$(BINARY_WINDOWS) $(CMD_AGENT)
	@echo "Done: $(BUILD_DIR)/$(BINARY_WINDOWS)"
	@echo "Size: $$(du -sh $(BUILD_DIR)/$(BINARY_WINDOWS) | cut -f1)"

## build-freebsd: build production binary for pfSense/FreeBSD amd64 → bin/plusclouds.freebsd
build-freebsd:
	@echo "Building $(BINARY_FREEBSD) $(VERSION) (freebsd/amd64)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=freebsd GOARCH=amd64 \
		go build -trimpath $(LDFLAGS_PROD) \
		-o $(BUILD_DIR)/$(BINARY_FREEBSD) $(CMD_AGENT)
	@echo "Done: $(BUILD_DIR)/$(BINARY_FREEBSD)"
	@echo "Size: $$(du -sh $(BUILD_DIR)/$(BINARY_FREEBSD) | cut -f1)"

## build-freebsd-arm64: build production binary for pfSense/FreeBSD arm64 (e.g. Netgate 1100/2100) → bin/plusclouds.freebsd-arm64
build-freebsd-arm64:
	@echo "Building $(BINARY_FREEBSD_ARM64) $(VERSION) (freebsd/arm64)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=freebsd GOARCH=arm64 \
		go build -trimpath $(LDFLAGS_PROD) \
		-o $(BUILD_DIR)/$(BINARY_FREEBSD_ARM64) $(CMD_AGENT)
	@echo "Done: $(BUILD_DIR)/$(BINARY_FREEBSD_ARM64)"
	@echo "Size: $$(du -sh $(BUILD_DIR)/$(BINARY_FREEBSD_ARM64) | cut -f1)"

## build-all: build production binaries for all platforms
build-all: build-linux build-windows build-freebsd build-freebsd-arm64
	@echo ""
	@echo "All platform binaries:"
	@ls -lh $(BUILD_DIR)/plusclouds.*

## test: run all tests with race detector and coverage
test:
	@echo "Running tests..."
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## lint: run golangci-lint
lint:
	@echo "Running linter..."
	golangci-lint run ./...

## clean: remove build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR) dist
	@rm -f coverage.out coverage.html
	@echo "Clean done."

## install: install binaries, systemd unit, and default config (requires root)
install: build-prod
	@echo "Installing $(BINARY_AGENT) to $(INSTALL_DIR)..."
	install -m 0755 $(BUILD_DIR)/$(BINARY_AGENT) $(INSTALL_DIR)/$(BINARY_AGENT)
	@echo "Installing $(BINARY_CTL) to $(INSTALL_DIR)..."
	install -m 0755 $(BUILD_DIR)/$(BINARY_CTL) $(INSTALL_DIR)/$(BINARY_CTL)
	@echo "Installing systemd unit..."
	install -m 0644 systemd/plusclouds-agent.service $(SERVICE_DIR)/plusclouds-agent.service
	@echo "Creating directories..."
	@mkdir -p $(CONFIG_DIR) $(LOG_DIR) $(ISO_MOUNT_DIR)
	@chmod 0750 $(LOG_DIR)
	systemctl daemon-reload
	@echo ""
	@echo "Install complete. Next steps:"
	@echo "  1. Attach the config-drive ISO (pc-meta-data.json) to this VM"
	@echo "  2. systemctl enable --now plusclouds-agent"
	@echo "  3. journalctl -fu plusclouds-agent"

## uninstall: remove binaries and systemd unit (does not remove config or logs)
uninstall:
	systemctl disable --now plusclouds-agent 2>/dev/null || true
	rm -f $(INSTALL_DIR)/$(BINARY_AGENT) $(INSTALL_DIR)/$(BINARY_CTL)
	rm -f $(SERVICE_DIR)/plusclouds-agent.service
	systemctl daemon-reload
	@echo "Uninstall complete. Config and logs preserved."

## package-deb: build a .deb package for distribution
package-deb: build-prod
	@echo "Packaging .deb ($(VERSION))..."
	@mkdir -p dist/deb/DEBIAN
	@mkdir -p dist/deb/usr/local/bin
	@mkdir -p dist/deb/etc/systemd/system
	@mkdir -p dist/deb/etc/plusclouds
	@mkdir -p dist/deb/var/log/plusclouds
	@mkdir -p dist/deb/media/plusclouds-config
	cp $(BUILD_DIR)/$(BINARY_AGENT) dist/deb/usr/local/bin/
	cp $(BUILD_DIR)/$(BINARY_CTL)   dist/deb/usr/local/bin/
	cp systemd/plusclouds-agent.service dist/deb/etc/systemd/system/
	@printf "Package: plusclouds-agent\n\
Version: $(VERSION)\n\
Section: utils\n\
Priority: optional\n\
Architecture: amd64\n\
Maintainer: PlusClouds <support@plusclouds.com>\n\
Description: PlusClouds Ubuntu VM Agent\n\
 A daemon for managing PlusClouds VMs via NATS.\n" > dist/deb/DEBIAN/control
	@printf "#!/bin/sh\n\
set -e\n\
mkdir -p /var/log/plusclouds /media/plusclouds-config\n\
chmod 0750 /var/log/plusclouds\n\
systemctl daemon-reload\n\
systemctl enable plusclouds-agent\n" > dist/deb/DEBIAN/postinst
	@chmod 0755 dist/deb/DEBIAN/postinst
	@printf "#!/bin/sh\n\
set -e\n\
systemctl disable --now plusclouds-agent 2>/dev/null || true\n" > dist/deb/DEBIAN/prerm
	@chmod 0755 dist/deb/DEBIAN/prerm
	dpkg-deb --build dist/deb dist/plusclouds-agent_$(VERSION)_amd64.deb
	@echo "Package built: dist/plusclouds-agent_$(VERSION)_amd64.deb"

## help: display this help
help:
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'

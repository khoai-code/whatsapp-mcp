# WhatsApp MCP: local install for one person on macOS.
SHELL := /bin/bash

REPO_DIR   := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
BRIDGE_DIR := $(REPO_DIR)/whatsapp-bridge
SERVER_DIR := $(REPO_DIR)/whatsapp-mcp-server
BRIDGE_BIN := $(BRIDGE_DIR)/whatsapp-bridge

LABEL      := com.khoai.whatsapp-bridge
PLIST      := $(HOME)/Library/LaunchAgents/$(LABEL).plist
LOG_FILE   := $(HOME)/Library/Logs/whatsapp-bridge.log
DOMAIN     := gui/$(shell id -u)

# True once whatsmeow has stored a paired device.
PAIRED     := [ -n "$$(sqlite3 $(BRIDGE_DIR)/store/whatsapp.db 'SELECT jid FROM whatsmeow_device LIMIT 1' 2>/dev/null)" ]

.PHONY: help check build sync config install pair start stop restart status logs test uninstall

help:
	@echo "make install    build, set up config, start the bridge in the background"
	@echo "make pair       link your phone (scan a QR code); run once, and again to re-pair"
	@echo "make status     is the bridge running and paired?"
	@echo "make logs       follow the bridge log"
	@echo "make restart    restart the bridge (after editing .env)"
	@echo "make test       run all tests"
	@echo "make uninstall  stop and remove the background bridge"

check:
	@command -v go >/dev/null || { echo "Go is missing: brew install go"; exit 1; }
	@command -v uv >/dev/null || { echo "uv is missing: brew install uv"; exit 1; }
	@xcode-select -p >/dev/null 2>&1 || { echo "Xcode command line tools missing: xcode-select --install"; exit 1; }

build: check
	cd $(BRIDGE_DIR) && go build -o whatsapp-bridge .

sync: check
	cd $(SERVER_DIR) && uv sync

config:
	@if [ ! -f $(REPO_DIR)/.env ]; then \
		cp $(REPO_DIR)/.env.example $(REPO_DIR)/.env && chmod 600 $(REPO_DIR)/.env && \
		echo "Created .env (read-only mode)"; \
	fi

$(PLIST): scripts/$(LABEL).plist.in
	@mkdir -p $(dir $(PLIST)) $(dir $(LOG_FILE))
	sed -e 's|@BRIDGE_DIR@|$(BRIDGE_DIR)|g' -e 's|@LOG_FILE@|$(LOG_FILE)|g' $< > $@

install: build sync config $(PLIST)
	@$(MAKE) --no-print-directory start
	@echo
	@echo "Installed. Next steps:"
	@echo
	@if $(PAIRED); then echo "1. Phone already linked."; else echo "1. Link your phone:  make pair"; fi
	@echo
	@echo "2. Register with Claude Code:"
	@echo "   claude mcp add --scope user whatsapp -- $$(command -v uv) --directory $(SERVER_DIR) run main.py"
	@echo
	@echo "   Or Claude Desktop: add this under \"mcpServers\" in"
	@echo "   ~/Library/Application Support/Claude/claude_desktop_config.json"
	@echo '   "whatsapp": {'
	@echo '     "command": "'$$(command -v uv)'",'
	@echo '     "args": ["--directory", "$(SERVER_DIR)", "run", "main.py"]'
	@echo '   }'
	@echo
	@echo "Read-only by default. See README.md to turn on sending."

# Pairing needs a terminal for the QR code, so stop the background bridge,
# run it in the foreground, then hand back to launchd.
pair: build config
	@launchctl bootout $(DOMAIN)/$(LABEL) 2>/dev/null || true
	@echo "On your phone: WhatsApp > Settings > Linked devices > Link a device, then scan."
	@echo "When you see 'Connected to WhatsApp', press Ctrl+C."
	@echo
	@# Ctrl+C also interrupts make, so the hand-back to launchd runs in the same
	@# shell, which ignores the interrupt and carries on once the bridge exits.
	@cd $(BRIDGE_DIR) && trap 'echo' INT; ./whatsapp-bridge; \
	if [ -f $(PLIST) ]; then \
		launchctl bootout $(DOMAIN)/$(LABEL) 2>/dev/null; \
		launchctl bootstrap $(DOMAIN) $(PLIST) && \
		echo "Bridge running in the background. Log: $(LOG_FILE)"; \
		echo "(make may print 'Interrupt' below; that's expected.)"; \
	fi

start: $(PLIST)
	@launchctl bootout $(DOMAIN)/$(LABEL) 2>/dev/null || true
	launchctl bootstrap $(DOMAIN) $(PLIST)
	@echo "Bridge running in the background. Log: $(LOG_FILE)"

stop:
	-launchctl bootout $(DOMAIN)/$(LABEL)

restart:
	launchctl kickstart -k $(DOMAIN)/$(LABEL)

status:
	@if launchctl print $(DOMAIN)/$(LABEL) >/dev/null 2>&1; then \
		launchctl print $(DOMAIN)/$(LABEL) | grep -E '^\s+(state|pid|last exit code) ='; \
	else \
		echo "Bridge is not installed as a background service (run make install)."; \
	fi
	@if $(PAIRED); then echo "Phone: linked"; else echo "Phone: not linked (run make pair)"; fi
	@tail -n 3 $(LOG_FILE) 2>/dev/null || true

logs:
	tail -f $(LOG_FILE)

test: check
	cd $(BRIDGE_DIR) && go test ./...
	cd $(SERVER_DIR) && uv run pytest -q

uninstall:
	-launchctl bootout $(DOMAIN)/$(LABEL) 2>/dev/null
	rm -f $(PLIST)
	@echo "Background bridge removed."
	@if [ -d $(BRIDGE_DIR)/store ]; then \
		read -r -p "Also delete your WhatsApp session and message history in whatsapp-bridge/store? [y/N] " ans; \
		if [ "$$ans" = "y" ] || [ "$$ans" = "Y" ]; then rm -rf $(BRIDGE_DIR)/store && echo "Deleted."; \
		else echo "Kept $(BRIDGE_DIR)/store."; fi; \
	fi
	@echo
	@echo "Also remove this computer from your phone: WhatsApp > Settings > Linked devices."
	@echo "And from Claude Code: claude mcp remove --scope user whatsapp"

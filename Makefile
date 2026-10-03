BIN           = register
PREFIX       ?= $(HOME)/bin
LIBEXEC_DIR   = $(PREFIX)/libexec
BUILD_DIR     = $(CURDIR)/.build
SERVICES_DIR ?= $(HOME)/Library/Services
QA_DIR        = $(CURDIR)/quickaction
WORKFLOWS     = add-to-binder binder-of

# The metadata engine is a separate project. Pin the tag the checks run against;
# bump it deliberately rather than drifting to whatever main happens to be.
FILEANCHOR_REPO    ?= https://github.com/rhsev/fileanchor.git
FILEANCHOR_VERSION ?= 1.2.0

# The Markdown layer is read through grubber, so the album checks need it the
# same way half the suite needs the engine. Same rule: pin a tag.
GRUBBER_VERSION ?= v0.18.0

# Stamp the version from the tag rather than trusting a literal in main.go,
# which shipped 1.4.0 as 1.3.0. A dirty or untagged tree says so in --version.
VERSION := $(shell git describe --tags --dirty --always 2>/dev/null | sed 's/^v//')
LDFLAGS := -X main.registerVersion=$(VERSION)

.PHONY: build fileanchor grubber link unlink install uninstall install-services uninstall-services test

build:
	go build -ldflags="$(LDFLAGS)" -o $(CURDIR)/$(BIN) ./cmd/register

# Build the fileanchor engine from source at the pinned tag. Needs a Swift
# toolchain (Xcode or the Command Line Tools); macOS arm64, 13 or later.
fileanchor:
	@rm -rf $(BUILD_DIR)/fileanchor-src
	@git clone --depth 1 --branch $(FILEANCHOR_VERSION) $(FILEANCHOR_REPO) $(BUILD_DIR)/fileanchor-src
	@cd $(BUILD_DIR)/fileanchor-src && swift build -c release
	@install -m 755 $(BUILD_DIR)/fileanchor-src/.build/release/fileanchor $(BUILD_DIR)/fileanchor
	@echo "→ $(BUILD_DIR)/fileanchor ($(FILEANCHOR_VERSION))"

# Fetch grubber's published release binary for this machine, not a source
# build. The release assets carry the right version string (a source build of
# v0.18.0 reports 0.16.0: the tag still has the literal as a const, which
# -ldflags cannot override), and grubber needs Go 1.27 to build while this repo
# pins an older one — a download keeps the two toolchains apart.
GRUBBER_ARCH = $(subst x86_64,amd64,$(shell uname -m))
grubber:
	@mkdir -p $(BUILD_DIR)
	@curl -fsSL -o $(BUILD_DIR)/grubber \
		https://github.com/rhsev/grubber/releases/download/$(GRUBBER_VERSION)/grubber-macos-$(GRUBBER_ARCH)
	@chmod 755 $(BUILD_DIR)/grubber
	@echo "→ $(BUILD_DIR)/grubber ($$($(BUILD_DIR)/grubber --version))"

# `install` copies, and stays that way: this repo is published, and an
# outsider's clone is not a stable location to point a symlink at. On the
# development machine use `make link` instead.
#
# `register` is a single self-contained Go binary. The engine is installed
# alongside it under libexec/, which is where the binary looks when neither
# $FILEANCHOR nor a `fileanchor` on PATH resolves. Run `make fileanchor` first,
# or install the engine yourself and skip that step.
install: build
	install -d $(PREFIX)
	install -m 755 $(CURDIR)/$(BIN) $(PREFIX)/$(BIN)
	@if [ -x $(BUILD_DIR)/fileanchor ]; then \
		install -d $(LIBEXEC_DIR); \
		install -m 755 $(BUILD_DIR)/fileanchor $(LIBEXEC_DIR)/fileanchor; \
		echo "installed $(PREFIX)/register + $(LIBEXEC_DIR)/fileanchor"; \
	else \
		echo "installed $(PREFIX)/register"; \
		echo "note: no engine installed — put fileanchor on PATH, set \$$FILEANCHOR, or run 'make fileanchor && make install'"; \
	fi

# Development machine: link once, then `make build` is all that deploying takes.
link: build
	@install -d $(PREFIX)
	@ln -sfn $(CURDIR)/$(BIN) $(PREFIX)/$(BIN)
	@echo "linked $(PREFIX)/$(BIN) -> $(CURDIR)/$(BIN)"

unlink:
	rm -f $(PREFIX)/$(BIN)

uninstall:
	rm -f $(PREFIX)/register
	rm -f $(LIBEXEC_DIR)/fileanchor
	-rmdir $(LIBEXEC_DIR) 2>/dev/null || true

# Finder Quick Actions. The workflows call the scripts in this repo (the repo
# path is substituted at install time); adjust the environment in
# ~/.config/fileregister/quickaction.env — see quickaction/README.md.
# enable-quickactions.sh writes the pbs activation entry (without it the actions
# register but stay invisible in the context menu) and restarts the Finder.
install-services:
	@for wf in $(WORKFLOWS); do \
		dest="$(SERVICES_DIR)/fileregister-$$wf.workflow"; \
		rm -rf "$$dest"; \
		cp -R "$(QA_DIR)/$$wf.workflow" "$$dest"; \
		sed -i '' "s|@@QA_DIR@@|$(QA_DIR)|g" "$$dest/Contents/document.wflow"; \
		echo "installed $$dest"; \
	done
	@$(QA_DIR)/enable-quickactions.sh $(foreach wf,$(WORKFLOWS),"$(SERVICES_DIR)/fileregister-$(wf).workflow")

uninstall-services:
	@for wf in $(WORKFLOWS); do rm -rf "$(SERVICES_DIR)/fileregister-$$wf.workflow"; done

# Unit tests plus self-contained golden tests (cmd/register/testdata/*.golden).
# Roughly half the suite drives a live engine and SKIPS without one, so run
# `make fileanchor` first for a meaningful pass. Regenerate golden files after an
# intended change with: go test -update ./...
test:
	go test ./...

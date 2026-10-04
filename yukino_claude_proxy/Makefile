.PHONY: build install test race vet release

RELEASE_TAG := yukino-claude-proxy
RELEASE_ASSETS := $(addprefix bin/yukino-claude-proxy-,linux-x64 linux-arm64 darwin-x64 darwin-arm64 win32-x64 win32-arm64)

build:
	go build -o bin/yukino-claude-proxy ./cmd/yukino-claude-proxy

install:
	go install ./cmd/yukino-claude-proxy

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

release:
	@command -v gh >/dev/null || { echo "GitHub CLI is required; install gh and run gh auth login." >&2; exit 1; }
	node build.mjs
	@set -eu; \
	commit="$$(git rev-parse HEAD)"; \
	if gh release view "$(RELEASE_TAG)" >/dev/null 2>&1; then \
		gh release upload "$(RELEASE_TAG)" $(RELEASE_ASSETS) --clobber; \
		gh api --method PATCH "repos/{owner}/{repo}/git/refs/tags/$(RELEASE_TAG)" \
			--raw-field "sha=$$commit" --field force=true --silent; \
		gh release edit "$(RELEASE_TAG)" --title "$(RELEASE_TAG)" --target "$$commit" \
			--notes "Standalone executables for Linux, macOS, and Windows on x64 and arm64." --latest; \
	else \
		gh release create "$(RELEASE_TAG)" $(RELEASE_ASSETS) --title "$(RELEASE_TAG)" \
			--target "$$commit" --notes "Standalone executables for Linux, macOS, and Windows on x64 and arm64." --latest; \
	fi

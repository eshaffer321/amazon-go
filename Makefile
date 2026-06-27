GO ?= go
GOFMT ?= gofmt
PKGS := ./...

.PHONY: all fmt fmt-check tidy tidy-check build vet test test-race check

all: check

fmt:
	$(GOFMT) -w $$(find . -name '*.go' -not -path './.git/*')

fmt-check:
	@files="$$($(GOFMT) -l $$(find . -name '*.go' -not -path './.git/*'))"; \
	if [ -n "$$files" ]; then \
		echo "These files need gofmt:"; \
		echo "$$files"; \
		exit 1; \
	fi

tidy:
	$(GO) mod tidy

tidy-check:
	@tmp="$$(mktemp -d)"; \
	cp go.mod "$$tmp/go.mod"; \
	cp go.sum "$$tmp/go.sum"; \
	$(GO) mod tidy; \
	if ! cmp -s go.mod "$$tmp/go.mod" || ! cmp -s go.sum "$$tmp/go.sum"; then \
		echo "go.mod or go.sum changed after go mod tidy"; \
		diff -u "$$tmp/go.mod" go.mod || true; \
		diff -u "$$tmp/go.sum" go.sum || true; \
		rm -rf "$$tmp"; \
		exit 1; \
	fi; \
	rm -rf "$$tmp"

build:
	$(GO) build $(PKGS)

vet:
	$(GO) vet $(PKGS)

test:
	$(GO) test $(PKGS)

test-race:
	$(GO) test -race $(PKGS)

check: fmt-check tidy-check build vet test-race

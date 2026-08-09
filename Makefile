BINS := $(patsubst cmd/%,%,$(wildcard cmd/*))
PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
LDFLAGS ?= -s -w

.PHONY: all build deploy test clean $(BINS)

all: test build

build: $(BINS)

$(BINS):
	go build -ldflags '$(LDFLAGS)' -o bin/$@ ./cmd/$@

deploy: build
	mkdir -p $(BINDIR)
	for bin in $(BINS); do install -m 0755 bin/$$bin $(BINDIR)/$$bin; done

test:
	go test ./...

clean:
	rm -rf bin

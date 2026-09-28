BINARY  := hotspot
PREFIX  ?= /usr/local
GO      ?= go

.PHONY: build test vet fmt clean install run

build:
	CGO_ENABLED=0 $(GO) build -buildvcs=false -trimpath -ldflags "-s -w" -o $(BINARY) ./cmd/hotspot

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .

clean:
	rm -f $(BINARY)

install: build
	install -Dm755 $(BINARY) $(DESTDIR)$(PREFIX)/bin/$(BINARY)

run: build
	./$(BINARY) sources

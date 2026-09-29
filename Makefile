BINARY  := hotspot
PREFIX  ?= /usr/local
GO      ?= go

.PHONY: build test vet fmt clean install install-local uninstall-local release snapshot run

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
	rm -rf dist

# Instala en el sistema (requiere sudo por PREFIX=/usr/local).
install: build
	install -Dm755 $(BINARY) $(DESTDIR)$(PREFIX)/bin/$(BINARY)

# Instala el paquete completo en el HOME del usuario, sin sudo:
# binario + función fish + completions.
install-local: build
	install -Dm755 $(BINARY) $(HOME)/.local/bin/$(BINARY)
	install -Dm644 functions/hotspot.fish $(HOME)/.config/fish/functions/hotspot.fish
	install -Dm644 completions/hotspot.fish $(HOME)/.config/fish/completions/hotspot.fish

uninstall-local:
	rm -f $(HOME)/.local/bin/$(BINARY)
	rm -f $(HOME)/.config/fish/functions/hotspot.fish
	rm -f $(HOME)/.config/fish/completions/hotspot.fish

# Construye artefactos de release localmente sin publicar (necesita goreleaser).
snapshot:
	goreleaser release --snapshot --clean

# Publica una release (normalmente lo hace CI al empujar un tag vX.Y.Z).
release:
	goreleaser release --clean

run: build
	./$(BINARY) sources

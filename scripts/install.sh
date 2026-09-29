#!/usr/bin/env sh
# hotspot — instalador de paquete único (binario precompilado + fish).
#
# Descarga el binario de un GitHub Release (el repo es privado, así que usa
# `gh`, ya autenticado con `gh auth login`) y lo deja en ~/.local/bin, además
# de instalar la función y las completions de fish si están disponibles.
#
# Uso:
#   ./scripts/install.sh            # instala la última release
#   ./scripts/install.sh v0.2.0     # instala una versión concreta
#   ./scripts/install.sh --uninstall
#
# Requisitos en runtime (no los instala aquí): hostapd, dnsmasq, nftables,
# iproute2, iw y root para up/down/tui.

set -eu

REPO="luismasuarez/hotspot"
BIN_DIR="${HOTSPOT_BIN_DIR:-$HOME/.local/bin}"
FISH_FUNCTIONS="${XDG_CONFIG_HOME:-$HOME/.config}/fish/functions"
FISH_COMPLETIONS="${XDG_CONFIG_HOME:-$HOME/.config}/fish/completions"
VERSION="${1:-}"

say()  { printf '%s\n' "$*"; }
warn() { printf '%s\n' "$*" >&2; }
die()  { warn "error: $*"; exit 1; }

uninstall() {
    say "→ desinstalando hotspot"
    rm -f "$BIN_DIR/hotspot" "$FISH_FUNCTIONS/hotspot.fish" "$FISH_COMPLETIONS/hotspot.fish"
    say "✓ eliminado de $BIN_DIR y fish"
    exit 0
}

[ "${VERSION:-}" = "--uninstall" ] && uninstall

# --- detección de plataforma -------------------------------------------------
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
    linux|darwin) ;;
    *) die "SO no soportado: $os (solo linux/darwin por ahora)" ;;
esac
case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) die "arquitectura no soportada: $(uname -m)" ;;
esac

command -v gh >/dev/null 2>&1 || die "falta 'gh' (GitHub CLI). Instálalo y ejecuta 'gh auth login'."

# --- resolución de la versión ------------------------------------------------
if [ -z "$VERSION" ]; then
    VERSION=$(gh release view --repo "$REPO" --json tagName -q .tagName 2>/dev/null) \
        || die "no hay releases en $REPO; pasa una versión: ./scripts/install.sh v0.2.0"
fi
VERSION="${VERSION#v}" # por si pasan vX.Y.Z

asset="hotspot_${VERSION}_${os}_${arch}.tar.gz"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

say "→ descargando hotspot $VERSION ($os/$arch) vía gh"
gh release download "v$VERSION" --repo "$REPO" --pattern "$asset" --dir "$tmp" \
    || die "no se pudo descargar $asset del release v$VERSION"

# checksums (si el release los publica)
if gh release download "v$VERSION" --repo "$REPO" --pattern checksums.txt --dir "$tmp" 2>/dev/null; then
    if command -v sha256sum >/dev/null 2>&1; then
        ( cd "$tmp" && grep " $asset\$" checksums.txt | sha256sum -c - >/dev/null ) \
            || die "el checksum de $asset no coincide"
        say "✓ checksum verificado"
    fi
fi

tar -xzf "$tmp/$asset" -C "$tmp"

# --- instalación -------------------------------------------------------------
mkdir -p "$BIN_DIR"
install -m 0755 "$tmp/hotspot" "$BIN_DIR/hotspot"
say "✓ binario instalado en $BIN_DIR/hotspot"

# fish (función + completions): del archivo extraído o del propio repo
fish_dir="$(cd "$(dirname "$0")/.." 2>/dev/null && pwd || true)"
if [ -n "$fish_dir" ] && [ -f "$fish_dir/functions/hotspot.fish" ]; then
    src_fun="$fish_dir/functions/hotspot.fish"
    src_cmp="$fish_dir/completions/hotspot.fish"
elif [ -f "$tmp/functions/hotspot.fish" ]; then
    src_fun="$tmp/functions/hotspot.fish"
    src_cmp="$tmp/completions/hotspot.fish"
fi

if [ -n "${src_fun:-}" ] && command -v fish >/dev/null 2>&1; then
    mkdir -p "$FISH_FUNCTIONS" "$FISH_COMPLETIONS"
    install -m 0644 "$src_fun" "$FISH_FUNCTIONS/hotspot.fish"
    install -m 0644 "$src_cmp" "$FISH_COMPLETIONS/hotspot.fish"
    say "✓ fish actualizado (función + completions)"
fi

# --- PATH y requisitos -------------------------------------------------------
case ":$PATH:" in
    *":$BIN_DIR:"*) ;;
    *) warn "aviso: $BIN_DIR no está en PATH; añádelo (p. ej. 'fish_add_path $BIN_DIR')" ;;
esac

missing=""
for tool in hostapd dnsmasq nft iw ip; do
    command -v "$tool" >/dev/null 2>&1 || missing="$missing $tool"
done
if [ -n "$missing" ]; then
    warn "aviso: faltan requisitos de runtime:$missing"
    warn "       en Debian/Ubuntu: sudo apt install hostapd dnsmasq nftables iproute2 iw"
fi

say ""
say "✓ hotspot $VERSION instalado. Prueba con:"
say "    hotspot version"
say "    sudo hotspot doctor"
say "    sudo hotspot up --source vpn --ssid WIFI_GRATIS"
say "    sudo hotspot tui"

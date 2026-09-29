# hotspot

[![CI](https://github.com/luismasuarez/hotspot/actions/workflows/ci.yml/badge.svg)](https://github.com/luismasuarez/hotspot/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/luismasuarez/hotspot)](https://github.com/luismasuarez/hotspot/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-%3E%3D1.26.2-00ADD8.svg)](go.mod)

CLI en Go que crea un **punto de acceso wifi** que comparte un **origen de
internet concreto** — ethernet, wifi o VPN — y permite **gestionar los
dispositivos conectados** (darles o cortarles el internet, impedir que se
conecten o expulsarlos) desde una TUI interactiva o por línea de comandos.

> **Solo Linux.** Es un orquestador de herramientas nativas de Linux (`hostapd`,
> `dnsmasq`, `nft`, `ip`, `iw`) y usa rutas como `/proc` o `/sys`. No funciona en
> Windows ni macOS (compila, pero no hay backend de red). Ver [Plataformas](#plataformas).

Caso de uso original: un equipo donde **todo el tráfico sale por una VPN**
(WireGuard) que los demás dispositivos de la LAN no pueden usar; `hotspot`
comparte ese túnel —o el ethernet— por wifi con quien tú decidas.

## Características

- **Tres orígenes** de internet: `vpn`, `eth` y `wifi` (AP+STA en el mismo radio).
- **TUI interactiva** (`hotspot tui`): dispositivos en vivo y switches por MAC
  (Internet ON/OFF, Permitir conexión ON/OFF, expulsar, renombrar) + QR.
- **Control por CLI** para todo (scriptable): `clients`, `block`, `deny`, `kick`…
- **Política persistente** por dispositivo en `/var/lib/hotspot/clients.json`,
  reaplicada al levantar el hotspot.
- **QR** estándar `WIFI:` para conectar el móvil sin teclear la contraseña.
- **Autodetección**: interfaces, canal, MTU y cadena de firewall (Docker/nft).

## Orígenes (`--source`)

| Origen | Ejemplo de interfaz | Qué comparte |
|--------|---------------------|--------------|
| `vpn` (por defecto) | `8nternational` | El internet "especial" de la VPN |
| `eth` | `enp2s0` | El internet normal del ethernet |
| `wifi` | `wlp3s0` | La wifi ya conectada (AP+STA, mismo canal) |

`sources` detecta las interfaces disponibles (ethernet, wifi, wireguard, tun) y
`up` resuelve cuál usar. Para `eth`/`vpn` el AP se levanta sobre la propia wifi;
para `wifi` se crea una interfaz AP virtual.

## Instalación

El instalador descarga el binario del [GitHub Release](https://github.com/luismasuarez/hotspot/releases/latest),
verifica el checksum e instala el binario en `~/.local/bin` y, si hay fish, la
función y las completions — todo **sin sudo**:

```sh
curl -sSL https://raw.githubusercontent.com/luismasuarez/hotspot/main/scripts/install.sh | sh
```

Desde el clone:

```sh
./scripts/install.sh              # última release
./scripts/install.sh v0.2.0       # versión concreta
./scripts/install.sh --uninstall
```

Alternativas:

```sh
make install-local   # compila e instala binario + fish en el HOME (sin sudo)
go install github.com/luismasuarez/hotspot/cmd/hotspot@latest   # nativo de Go
make install         # sistema completo en /usr/local/bin (necesita sudo)
```

> Con sudo, `sudo hotspot` puede no encontrar el binario (sudo resetea el `PATH`).
> Usa la ruta absoluta `sudo ~/.local/bin/hotspot ...` o añade `~/.local/bin` al
> `secure_path` de sudo (ver [Notas](#notas)).

## Uso

```sh
sudo hotspot doctor                       # comprueba requisitos del sistema
hotspot sources                           # lista orígenes disponibles
sudo hotspot up --source vpn --dry-run    # muestra el plan sin tocar nada
sudo hotspot up --source vpn --ssid WIFI_GRATIS
sudo hotspot tui                          # panel interactivo de dispositivos
hotspot clients                           # lista dispositivos conectados
sudo hotspot block <mac|ip>               # corta el internet a un dispositivo
sudo hotspot unblock <mac|ip>             # le devuelve el internet
sudo hotspot deny <mac|ip>                # impide que se conecte
sudo hotspot allow <mac|ip>               # vuelve a permitirlo
sudo hotspot kick <mac|ip>                # lo desconecta ahora
sudo hotspot qr                           # reimprime el QR del hotspot activo
sudo hotspot status                       # estado del hotspot
sudo hotspot down                         # revierte todos los cambios
```

Al levantar (`up`), el tool imprime un **QR** para que el móvil se conecte
escaneando. Desde la TUI, al arrancar el hotspot aparece directamente.

### Flags de `up`

| Flag | Defecto | Descripción |
|------|---------|-------------|
| `--source` | `vpn` | `eth`, `wifi` o `vpn` |
| `--iface` | autodetectada | interfaz de origen concreta |
| `--ssid` | `WIFI_GRATIS` | nombre del AP |
| `--pass` | aleatoria | contraseña WPA2 (mín. 8); si se omite se genera |
| `--open` | `false` | AP abierto, sin contraseña |
| `--band` | `2.4` | `2.4` o `5` |
| `--channel` | auto | canal del AP |
| `--subnet` | `10.42.42.0/24` | subred del hotspot |
| `--dns` | `8.8.8.8` | DNS entregado a los clientes |
| `--ap-iface` | `hspot0` | interfaz AP virtual (solo `--source wifi`) |
| `--mss` | `1360` | MSS para clamping TCP |
| `--mtu` | auto | MTU anunciado a clientes (auto = MTU del uplink) |
| `--hidden` | `false` | red oculta (SSID no difundido) |
| `--no-qr` | `false` | no imprimir el QR al levantar |
| `--qr-invert` | `false` | invertir colores del QR (temas oscuros) |
| `--qr-png <ruta>` | — | guardar además el QR como PNG |
| `--dry-run` | `false` | solo imprime los comandos |

## Gestión de dispositivos (TUI)

`sudo hotspot tui` abre un panel que **auto-refresca cada ~2 s** y muestra los
dispositivos conectados: nombre, IP, MAC, señal, tráfico (bajada/subida) y
tiempo conectado. Por cada dispositivo:

| Acción | Efecto |
|--------|--------|
| **Internet ON/OFF** | corta o restaura el internet sin expulsarlo del AP |
| **Permitir ON/OFF** | permite o deniega que el dispositivo se asocie |
| **kick** | lo desconecta ahora mismo |
| **renombrar** | le pone un nombre propio (persistente) |

El estado se guarda en `/var/lib/hotspot/clients.json` (0600) y **persiste entre
reinicios**; al levantar el hotspot se reaplica (`Reconcile`), a diferencia del
estado efímero de `/run`.

Los mismos controles existen sin TUI: `clients`, `block`, `unblock`, `deny`,
`allow` y `kick`.

**Mecanismos de bloqueo** (independientes y combinables):

1. **Internet OFF** — nft añade la MAC al set `blocked` (tipo `ether_addr`) de
   `table inet hotspot`; la regla `iifname <ap> ether saddr @blocked drop`
   descarta su tráfico. El dispositivo **sigue asociado** y el DNS local sigue
   respondiendo: es intencional.
2. **Permitir OFF (deny)** — hostapd añade la MAC a su lista de denegados (MAC
   ACL: `macaddr_acl=0` + `deny_mac_file`) y lo expulsa con `hostapd_cli
   deauthenticate`.
3. **kick** — solo `hostapd_cli deauthenticate`, sin cambiar la política.

## Cómo funciona (red)

1. **AP** levantado por `hostapd`. Para `eth`/`vpn` se usa la propia wifi; para
   `wifi` (AP+STA) se crea una interfaz virtual con MAC propia (si comparte la
   MAC, el kernel rechaza levantarla con `ENOTUNIQ`).
2. Subred aislada `10.42.42.0/24` (gateway `.1`), DHCP + DNS con `dnsmasq`.
3. **Enrutado**: el equipo enruta todo por política hacia la VPN. El tool crea
   una tabla propia (`4242`) con un default hacia el uplink elegido y una regla
   `priority 9000 from 10.42.42.0/24 lookup 4242` para los clientes. Así `eth`
   evita la VPN y `vpn` la fuerza. La tabla incluye además la ruta de la subred
   del AP (`10.42.42.0/24 dev <ap>`); si no, el tráfico **host→cliente** (p. ej.
   las respuestas DNS de dnsmasq con origen `10.42.42.1`) se iría por el uplink.
4. **nft** (`table inet hotspot`): `masquerade` sobre el uplink, *MSS clamping*
   (la VPN tiene MTU 1420) y el set `blocked` para el corte por MAC. Se
   **anuncia el MTU del uplink** por DHCP (`dhcp-option=26`).
5. **Forwarding**: Docker pone `iptables -P FORWARD DROP` y una cadena nft con
   `policy accept` no anula el DROP de otra cadena base; por eso se inserta un
   `ACCEPT` explícito en `DOCKER-USER` (o `FORWARD`) y se elimina en `down`.
6. **QR**: payload estándar `WIFI:T:...`.
7. **Estado** en `/run/hotspot/state.json` (0600); `down` deshace cada paso
   (mata daemons, borra nft, reglas de forwarding, regla/tabla de rutas, el AP,
   y restaura NetworkManager e `ip_forward`).

## Requisitos

- **Linux** con `hostapd`, `dnsmasq`, `nftables`, `iproute2`, `iw`.
  En Debian/Ubuntu: `sudo apt install hostapd dnsmasq nftables iproute2 iw`.
- Permisos de **root** (`sudo`) para aplicar cambios.
- Chip wifi que soporte **modo AP** (comprueba con `hotspot doctor`).

## Plataformas

| Plataforma | Estado |
|------------|--------|
| Linux (amd64/arm64) | ✅ soportado |
| macOS | ❌ compila, sin backend de red |
| Windows | ❌ no soportado (sin `hostapd`/`nft`; el AP nativo no permite esta gestión) |
| WSL2 | ❌ no accede al wifi del host |

## Desarrollo

```sh
make build      # binario estático ./hotspot
make test       # go test ./...
make vet        # go vet ./...
make snapshot   # artefactos de release locales (tar.gz/deb/rpm) sin publicar
```

- Requiere Go **>= 1.26.2** (fijado en `go.mod`).
- Dependencias: `github.com/skip2/go-qrcode` (QR) y, para la TUI, Bubble Tea
  v1.3.10, Bubbles v1.0.0 y Lip Gloss v1.1.0 (línea estable v1).

Las releases se publican con **GoReleaser** desde GitHub Actions al empujar un
tag semántico:

```sh
git tag v0.2.0
git push origin v0.2.0     # CI compila linux/darwin × amd64/arm64, genera
                           # tar.gz + .deb + .rpm + checksums y crea el release
```

## Notas

- **`sudo hotspot` no encuentra el binario**: sudo resetea el `PATH`
  (`secure_path`). Usa `sudo ~/.local/bin/hotspot ...`, o permite la ruta:
  ```sh
  echo 'Defaults secure_path="/home/USUARIO/.local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"' \
    | sudo tee /etc/sudoers.d/hotspot-path && sudo chmod 440 /etc/sudoers.d/hotspot-path && sudo visudo -c
  ```
- **`--source wifi` comparte el radio**: el AP queda en el mismo canal que la STA,
  lo que limita el rendimiento. El tool avisa del canal usado.
- **Tras un reinicio**, si quedara algo a medias, limpia manualmente la tabla nft
  `inet hotspot` y la regla `ip rule`.

## Licencia

[MIT](LICENSE) © Luis Suarez

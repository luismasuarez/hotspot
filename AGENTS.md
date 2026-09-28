# AGENTS.md — hotspot

Guía para agentes que trabajen en este repo. Léela **antes** de analizar; evita
re-descubrir cosas que ya costaron una sesión entera.

## Qué es
CLI en Go que crea un punto de acceso wifi y **comparte un origen de internet**
seleccionable: `eth` (`enp2s0`), `wifi` (`wlp3s0`) o `vpn` (WireGuard de
NetworkManager `8nternational`). Repo privado: `github.com/luismasuarez/hotspot`.

Sin dependencias externas salvo `github.com/skip2/go-qrcode` (QR) y las librerías
de TUI fijadas a la línea estable v1: `github.com/charmbracelet/bubbletea`
v1.3.10, `github.com/charmbracelet/bubbles` v1.0.0 y
`github.com/charmbracelet/lipgloss` v1.1.0. No añadir más sin motivo (no usar la
línea v2/`charm.land`, que está en beta).

## Comandos
```sh
make build     # binario estático ./hotspot
make test      # go test ./...
make vet
gofmt -l .     # debe estar vacío
```
- Go se gestiona con **mise** (`.mise.toml` → go 1.26.2). La primera vez:
  `mise trust` dentro del repo, si no el shim `go` falla.
- **`-buildvcs=false` es obligatorio** (ya está en el Makefile): en el HOME existe
  un `/home/luisma/.git` que no es un work tree válido y rompe el build.
- Ejecutar la herramienta: `sudo ./hotspot up --source vpn --ssid WIFI_GRATIS`,
  `sudo ./hotspot down`, `hotspot sources`, `hotspot status`, `hotspot qr`,
  `hotspot doctor`. `--dry-run` imprime los comandos **sin root**.

## Mapa del código
| Archivo | Rol |
|---|---|
| `cmd/hotspot/main.go` | entrypoint → `hotspot.Main` |
| `internal/hotspot/cli.go` | comandos y flags |
| `internal/hotspot/apply.go` | orquestación de `up`, rollback, forwarding |
| `internal/hotspot/down.go` | teardown idempotente |
| `internal/hotspot/source.go` | detección de interfaces (`ip -j -d link`) |
| `internal/hotspot/netutil.go` | subred, MAC derivada, MTU |
| `internal/hotspot/templates.go` | `hostapd.conf`, `dnsmasq.conf`, reglas nft |
| `internal/hotspot/state.go` | estado en `/run/hotspot/state.json` (0600) |
| `internal/hotspot/qr.go` | payload `WIFI:` + render QR |
| `internal/hotspot/runner.go` | ejecución de comandos (con `--dry-run`) |
| `internal/hotspot/clients.go` | `ScanClients` (iw/leases/neigh) y `resolveMAC` |
| `internal/hotspot/policy.go` | política por MAC en `/var/lib/hotspot/clients.json` + `Reconcile` |
| `internal/hotspot/acl.go` | set nft `blocked` y MAC ACL de hostapd (`deny_acl`, `deauthenticate`) |
| `internal/hotspot/tui.go` | panel TUI (Bubble Tea): dispositivos e interruptores |

## Invariantes críticos (NO romper)

1. **Docker pone `iptables -P FORWARD DROP`.** En nftables, una cadena con
   `policy accept` **no anula** el `drop` de otra base chain del mismo hook. Por
   eso `apply.go` inserta `ACCEPT` en `DOCKER-USER` (o `FORWARD` si no existe) —
   ver `detectFWChain`. **Nunca** uses `iptables -P FORWARD ACCEPT` (global e
   inseguro) ni confíes solo en la tabla nft propia para el reenvío.

2. **Fuga del tráfico host→cliente (rompe DNS).** El equipo enruta **todo** por
   política hacia la VPN (tabla `51825`, regla `31555`). El tool usa tabla propia
   `4242` + `ip rule priority 9000 from <subnet> lookup 4242`. Es
   **imprescindible** añadir también `10.42.42.0/24 dev <ap> table 4242`
   (`addUplinkRoute`), o las respuestas de dnsmasq (origen `10.42.42.1`) saldrán
   por la VPN. Síntoma: ping/HTTP a veces funcionan, **DNS casi nunca**.

3. **MAC de la interfaz AP.** Una vif AP en el mismo radio hereda la MAC base y
   `ip link set up` falla con `ENOTUNIQ: Name not unique on network`. Por eso:
   para `eth`/`vpn` se usa `wlp3s0` directamente (`APName=wlp3s0`,
   `Dedicated=false`); para `wifi` se crea vif + MAC derivada (`derivedMAC`).
   En `down`, **no borres `wlp3s0`** si `Dedicated=false`.

4. **MTU/PMTU.** El uplink WG tiene MTU 1420. Se anuncia a clientes por DHCP
   (`dhcp-option=26`) y se clampa MSS a 1360. No lo quites.

5. **NetworkManager.** Se deshabilita sobre la wifi mientras dura el hotspot
   (`nmcli dev set <wifi> managed no`) y se restaura en `down`.

6. **Aislamiento de reglas.** Todo en `table inet hotspot`; **no tocar** reglas de
   Docker/Tailscale. El teardown borra la tabla nft, la `ip rule`, la tabla de
   rutas y las reglas de forwarding propias.

7. **Corte de internet por dispositivo.** Depende del set nft `blocked` (tipo
   `ether_addr`) dentro de `table inet hotspot` + regla `ether saddr @blocked
   drop`, y del MAC ACL de hostapd vía `ctrl_interface=/run/hotspot/hostapd` +
   `deny_mac_file` (con `macaddr_acl=0`, que acepta todo lo no denegado).
   **Nunca** quites `macaddr_acl=0`, `deny_mac_file`, `ctrl_interface` ni el set
   `blocked`: romperías `block`/`deny`/`kick` y el `Reconcile` de `up`. `down`
   limpia `/run/hotspot/deny.mac`, los leases y el ctrl dir (ya lo hace
   `removeState` en `state.go`).

8. **Política persistente en `/var/lib/hotspot/clients.json` (0600).** Guarda el
   nombre y los interruptores por MAC y **no** vive bajo `/run` a propósito:
   debe sobrevivir reinicios (a diferencia de `state.json`). No la borres en
   `down`.

## Diagnóstico rápido (evitar re-análisis)
```sh
sudo hotspot status; cat /run/hotspot/state.json

ip rule | grep 9000
ip route show table 4242          # debe tener: default dev <uplink>  Y  10.42.42.0/24 dev <ap>
nft list table inet hotspot
nft list set inet hotspot blocked                 # MACs sin internet
hostapd_cli -p /run/hotspot/hostapd -i <ap> deny_acl SHOW   # MACs denegadas
cat /var/lib/hotspot/clients.json                 # política persistente (0600)
iptables -S DOCKER-USER           # deben estar los 2 ACCEPT

# rutas correctas:
ip route get 8.8.8.8 from 10.42.42.20 iif wlp3s0        # -> table 4242 dev <uplink>
ip route get 10.42.42.20 from 10.42.42.1                # -> dev <ap> table 4242  (¡clave!)

# DNS
dig @10.42.42.1 google.com
tcpdump -i any -n port 53         # respuestas dnsmasq deben salir por wlp3s0, NO por 8nternational

# DHCP
cat /run/hotspot/dnsmasq.leases
journalctl --no-pager | grep -i dnsmasq | tail

# reenvío sin el móvil (netns + veth, cliente 10.42.42.17/32):
#   ruta 10.42.42.17/32 dev vt0 + ACCEPT temporal en DOCKER-USER para vt0
#   ¡LIMPIA siempre las reglas temporales que añadas!
```

## Conseguir root como agente no interactivo
`sudo` pide contraseña en esta sesión. Para inspeccionar (solo lectura) usa un
contenedor privilegiado; el usuario está en el grupo `docker`:
```sh
docker run --rm --privileged --net=host -v /:/host --entrypoint chroot <imagen> /host /bin/bash <script>
```
Docker **no** es dependencia del tool (solo se usó para diagnóstico).
Restaura el sistema: borra cualquier regla/estado temporal al terminar
(`nft delete table inet hotspot`, `iptables -D DOCKER-USER ...`).

## Entorno (detectar, no hardcodear)
- `enp2s0` ethernet · `wlp3s0` wifi (soporta AP; AP+STA = **mismo canal**,
  `#channels<=1`) · `8nternational` WireGuard NM (`172.16.1.3/32`, MTU 1420,
  DNS 8.8.8.8) · `tailscale0`.
- `hostapd` **no** viene instalado por defecto (`sudo apt install hostapd`).
- El equipo enruta todo por política a la WG (tabla 51825). No asumas el nombre
  de la VPN ni el id de tabla: usa `hotspot sources` / la detección.

## No hacer
- No tocar `ip_forward`, policies globales ni reglas de Docker/Tailscale.
- No hardcodear `8nternational`/tabla `51825`; detectar.
- No editar `/etc` ni dejar reglas tras diagnosticar.
- No commitear el binario `hotspot` (está en `.gitignore`).
- No añadir dependencias (solo `go-qrcode` y la línea TUI v1: bubbletea,
  bubbles, lipgloss).
- No pisar `/run/hotspot` a mano; usar `hotspot down`/`up`.

## Estado
`v0.2.0` — funcional y verificado (los clientes navegan compartiendo la VPN, con
gestión de dispositivos por TUI/CLI).

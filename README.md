# hotspot

CLI en Go para crear un punto de acceso wifi que **comparte un origen de internet
concreto**: el ethernet, la wifi principal o una VPN. Pensado para el caso de este
equipo, donde todo el tráfico sale por una VPN (WireGuard `8nternational`) que los
demás dispositivos de la LAN no pueden usar.

## Orígenes (`--source`)

| Origen | Interfaz | Qué comparte |
|--------|----------|--------------|
| `vpn` (por defecto) | `8nternational` | El internet "especial" de la VPN |
| `eth` | `enp2s0` | El internet normal del ethernet |
| `wifi` | `wlp3s0` | La wifi ya conectada (AP+STA en el mismo radio, mismo canal) |

`sources` detecta automáticamente las interfaces disponibles (ethernet, wifi,
wireguard, tun) y `up` resuelve cuál usar para el origen pedido. Para `eth` y
`vpn` el AP se levanta sobre `wlp3s0` directamente (no requiere interfaz
virtual).

## Uso

```sh
sudo hotspot doctor                       # comprueba requisitos
hotspot sources                           # lista orígenes disponibles
sudo hotspot up --source vpn --dry-run    # muestra el plan sin tocar nada
sudo hotspot up --source vpn --ssid WIFI_GRATIS
sudo hotspot qr                           # reimprime el QR del hotspot activo
sudo hotspot qr --png ~/wifi.png          # además guarda el PNG
sudo hotspot status
sudo hotspot down                         # revierte todos los cambios
```

Al levantar, el tool imprime un **QR** para que el móvil se conecte sin teclear
la contraseña.

Flags de `up`:

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

## Cómo funciona (red)

1. **AP** levantado por `hostapd` sobre `wlp3s0`. Para `eth`/`vpn` se usa la
   propia `wlp3s0`; para `wifi` (AP+STA) se crea la interfaz virtual `hspot0`
   con una MAC propia (si comparte la MAC de `wlp3s0` el kernel rechaza
   levantarla con `ENOTUNIQ: Name not unique on network`).
2. Subred aislada `10.42.42.0/24` (gateway `.1`), DHCP + DNS con `dnsmasq`.
3. **Enrutado**: el equipo enruta todo por política hacia la VPN (regla
   `31555: not from all fwmark 0xca71 lookup 51825`). Por eso el tool crea una
   tabla propia (`4242`) con un default hacia el uplink elegido y una regla
   `priority 9000 from 10.42.42.0/24 lookup 4242` para los clientes. Así `eth`
   puede evitar la VPN y `vpn` la fuerza explícitamente. La tabla 4242 incluye
   además la ruta de la propia subred del AP (`10.42.42.0/24 dev <ap>`); si no,
   el tráfico **host→cliente** (p. ej. las respuestas DNS de dnsmasq con origen
   `10.42.42.1`) coincidiría con la regla y se iría por el uplink.
4. **nft** (`table inet hotspot`): `masquerade` sobre el uplink y
   *MSS clamping* (la VPN tiene MTU 1420) para evitar blackholes PMTU. Además
   se **anuncia el MTU del uplink** por DHCP (`dhcp-option=26`) para que los
   móviles no manden datagramas (QUIC/UDP) que la VPN tenga que fragmentar.
5. **Forwarding**: Docker pone `iptables -P FORWARD DROP` y una cadena nft con
   `policy accept` **no puede anular el DROP de otra cadena base**, por eso el
   tool inserta un `ACCEPT` explícito en `DOCKER-USER` (o `FORWARD`) para el
   tráfico `AP ↔ uplink` y lo elimina en `down`.
6. **QR**: payload estándar `WIFI:T:...` para conectar el móvil escaneando.
7. **Estado** en `/run/hotspot/state.json` (0600); `down` deshace cada paso
   (mata daemons, borra la tabla nft, las reglas de forwarding, la regla y la
   tabla de rutas, elimina el AP y restaura NetworkManager e `ip_forward`).

## Requisitos

- Linux con `hostapd`, `dnsmasq`, `nftables`, `iproute2`, `iw`.
- Permisos de root (`sudo`) para aplicar cambios.
- Chip wifi que soporte modo AP (comprueba con `hotspot doctor`).

## Build

```sh
make build      # binario estático ./hotspot
make test       # go test ./...
make vet        # go vet ./...
make install    # instala en /usr/local/bin
```

## Notas

- `--source wifi` comparte el radio: el AP queda en el mismo canal que la STA,
  lo que limita el rendimiento. El tool avisa del canal usado.
- `down` depende del estado en `/run`; tras un reinicio, si quedara algo a medias,
  limpia manualmente la tabla nft `inet hotspot` y la regla `ip rule`.

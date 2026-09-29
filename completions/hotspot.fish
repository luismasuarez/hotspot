# Completions fish para hotspot.
# Instalación: scripts/install.sh o `make install-local`.

function __hotspot_no_subcommand
    for i in (commandline -opc)
        if contains -- $i up down status qr sources doctor clients tui block unblock deny allow kick version help
            return 1
        end
    end
    return 0
end

# Subcomandos
complete -c hotspot -f -n '__hotspot_no_subcommand' -a up        -d 'Levanta el hotspot'
complete -c hotspot -f -n '__hotspot_no_subcommand' -a down      -d 'Detiene el hotspot'
complete -c hotspot -f -n '__hotspot_no_subcommand' -a status    -d 'Muestra el estado'
complete -c hotspot -f -n '__hotspot_no_subcommand' -a qr        -d 'Muestra el QR del hotspot'
complete -c hotspot -f -n '__hotspot_no_subcommand' -a sources   -d 'Lista orígenes de internet'
complete -c hotspot -f -n '__hotspot_no_subcommand' -a doctor    -d 'Comprueba requisitos'
complete -c hotspot -f -n '__hotspot_no_subcommand' -a clients   -d 'Lista dispositivos conectados'
complete -c hotspot -f -n '__hotspot_no_subcommand' -a tui       -d 'Panel interactivo'
complete -c hotspot -f -n '__hotspot_no_subcommand' -a block     -d 'Corta el internet a un dispositivo'
complete -c hotspot -f -n '__hotspot_no_subcommand' -a unblock   -d 'Devuelve el internet'
complete -c hotspot -f -n '__hotspot_no_subcommand' -a deny      -d 'Impide que se conecte'
complete -c hotspot -f -n '__hotspot_no_subcommand' -a allow     -d 'Permite que se conecte'
complete -c hotspot -f -n '__hotspot_no_subcommand' -a kick      -d 'Desconecta ahora'
complete -c hotspot -f -n '__hotspot_no_subcommand' -a version   -d 'Muestra la versión'
complete -c hotspot -f -n '__hotspot_no_subcommand' -a help      -d 'Ayuda'

# Flags de 'up'
complete -c hotspot -n '__fish_seen_subcommand_from up' -l source     -x -a 'eth wifi vpn' -d 'Origen de internet'
complete -c hotspot -n '__fish_seen_subcommand_from up' -l iface      -r -d 'Interfaz de origen'
complete -c hotspot -n '__fish_seen_subcommand_from up' -l ssid       -r -d 'Nombre del hotspot'
complete -c hotspot -n '__fish_seen_subcommand_from up' -l pass       -r -d 'Contraseña WPA2'
complete -c hotspot -n '__fish_seen_subcommand_from up' -l open       -d 'AP abierto (sin contraseña)'
complete -c hotspot -n '__fish_seen_subcommand_from up' -l band       -x -a '2.4 5' -d 'Banda del AP'
complete -c hotspot -n '__fish_seen_subcommand_from up' -l channel    -r -d 'Canal del AP'
complete -c hotspot -n '__fish_seen_subcommand_from up' -l subnet     -r -d 'Subred CIDR'
complete -c hotspot -n '__fish_seen_subcommand_from up' -l dns        -r -d 'DNS para clientes'
complete -c hotspot -n '__fish_seen_subcommand_from up' -l ap-iface   -r -d 'Interfaz AP virtual'
complete -c hotspot -n '__fish_seen_subcommand_from up' -l table      -r -d 'Tabla de enrutado'
complete -c hotspot -n '__fish_seen_subcommand_from up' -l rule-priority -r -d 'Prioridad de la regla'
complete -c hotspot -n '__fish_seen_subcommand_from up' -l mss        -r -d 'MSS máximo'
complete -c hotspot -n '__fish_seen_subcommand_from up' -l mtu        -r -d 'MTU anunciado'
complete -c hotspot -n '__fish_seen_subcommand_from up' -l hidden     -d 'Red oculta'
complete -c hotspot -n '__fish_seen_subcommand_from up' -l no-qr      -d 'No imprimir el QR'
complete -c hotspot -n '__fish_seen_subcommand_from up' -l qr-invert  -d 'Invertir colores del QR'
complete -c hotspot -n '__fish_seen_subcommand_from up' -l qr-png     -r -d 'Guardar QR como PNG'
complete -c hotspot -n '__fish_seen_subcommand_from up' -l dry-run    -d 'Solo mostrar los comandos'

# Flags de 'down' y 'qr'
complete -c hotspot -n '__fish_seen_subcommand_from down' -l dry-run  -d 'Solo mostrar los comandos'
complete -c hotspot -n '__fish_seen_subcommand_from qr'   -l invert   -d 'Invertir colores del QR'
complete -c hotspot -n '__fish_seen_subcommand_from qr'   -l png      -r -d 'Guardar QR como PNG'

# Dispositivos: completa con las IPs activas (silencioso si falla)
complete -c hotspot -f -n '__fish_seen_subcommand_from block unblock deny allow kick' -a '(hotspot clients 2>/dev/null | string match -r "^\S+\s+(\S+)" | string replace -r "^\S+\s+(\S+).*" "\$1")'

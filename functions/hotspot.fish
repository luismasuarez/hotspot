# hotspot — envoltorio fish para no navegar a la carpeta del repo.
#
# El binario vive en ~/.local/bin/hotspot (o donde esté en PATH). Esta función
# solo lo invoca y añade ayudas de ergonomía: el uso de sudo es EXPLÍCITO, no lo
# inyecta por ti. Instalación: scripts/install.sh o `make install-local`.
function hotspot --description 'Hotspot wifi que comparte eth/wifi/vpn (TUI + CLI)'
    set -l bin (command -s hotspot)
    if test -z "$bin"
        echo "hotspot: no encuentro el binario en PATH. Instálalo con scripts/install.sh" >&2
        return 127
    end

    # Subcomandos que exigen root; si falta, avisa con el comando exacto.
    set -l needs_root up down tui block unblock deny allow kick
    set -l cmd $argv[1]
    if contains -- $cmd $needs_root
        if test (id -u) -ne 0
            echo "hotspot: «$cmd» requiere root. Ejecuta: sudo hotspot $argv" >&2
            return 1
        end
    end

    command $bin $argv
end

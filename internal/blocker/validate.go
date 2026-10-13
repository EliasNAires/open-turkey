package blocker

import (
	"fmt"
	"strings"
)

// minProcessNameLen e o tamanho minimo de um nome de processo bloqueavel.
// Nomes muito curtos ("sh", "X") tem alta chance de pegar algo do sistema.
const minProcessNameLen = 3

// criticalProcesses sao processos que o Open Turkey nunca deve matar: o daemon
// roda como root e mata com SIGKILL, entao bloquear um deles derrubaria a
// sessao grafica, o login, a rede ou o proprio bloqueador. A comparacao e
// case-insensitive.
var criticalProcesses = map[string]bool{
	// init / systemd
	"systemd": true, "init": true, "kthreadd": true, "udevd": true,
	"dbus-daemon": true, "dbus-broker": true, "dbus-broker-launch": true,
	"polkitd": true, "rtkit-daemon": true,
	// login, privilegios e shells
	"login": true, "agetty": true, "sshd": true, "ssh": true, "ssh-agent": true,
	"sudo": true, "su": true, "doas": true, "pkexec": true,
	"bash": true, "sh": true, "dash": true, "zsh": true, "fish": true,
	"tmux": true, "screen": true,
	// display managers e servidores graficos
	"xorg": true, "x": true, "xwayland": true, "gdm": true, "gdm-session-worker": true,
	"sddm": true, "sddm-helper": true, "lightdm": true, "ly": true, "greetd": true, "uwsm": true,
	// compositores e shells graficos
	"gnome-shell": true, "mutter": true, "kwin": true, "kwin_x11": true, "kwin_wayland": true,
	"plasmashell": true, "hyprland": true, "sway": true, "weston": true,
	"quickshell": true, "qs": true, "waybar": true,
	// bloqueio de tela / idle (mata-los destravaria a tela)
	"hyprlock": true, "hypridle": true, "swaylock": true, "swayidle": true,
	// audio e rede
	"pipewire": true, "pipewire-pulse": true, "wireplumber": true, "pulseaudio": true,
	"networkmanager": true, "wpa_supplicant": true, "iwd": true, "dhcpcd": true,
	// o proprio bloqueador e o firewall
	"open-turkey": true, "open-turkey-bin": true, "iptables": true, "nft": true,
}

// criticalPrefixes cobrem familias inteiras de processos do sistema.
var criticalPrefixes = []string{"systemd-", "kworker", "ksoftirqd", "migration/"}

// ValidarNomeProcesso rejeita nomes que nao devem ir para a lista de apps
// bloqueados: vazios, curtos demais, com "/" ou espacos (o match e por
// basename exato, entao nunca bateriam como esperado) ou de processos criticos.
func ValidarNomeProcesso(nome string) error {
	if strings.TrimSpace(nome) == "" {
		return fmt.Errorf("nome de processo vazio")
	}
	if nome != strings.TrimSpace(nome) || strings.ContainsAny(nome, " \t\n/") {
		return fmt.Errorf("nome de processo '%s' invalido: use so o nome do executavel, sem caminho nem espacos", nome)
	}
	if len(nome) < minProcessNameLen {
		return fmt.Errorf("nome de processo '%s' curto demais (minimo %d caracteres)", nome, minProcessNameLen)
	}
	lower := strings.ToLower(nome)
	if criticalProcesses[lower] {
		return fmt.Errorf("'%s' e um processo critico do sistema e nao pode ser bloqueado", nome)
	}
	for _, p := range criticalPrefixes {
		if strings.HasPrefix(lower, p) {
			return fmt.Errorf("'%s' e um processo critico do sistema (%s*) e nao pode ser bloqueado", nome, p)
		}
	}
	return nil
}

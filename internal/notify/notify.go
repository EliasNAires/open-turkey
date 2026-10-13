// Package notify mostra notificacoes do desktop Omarchy a partir de um
// processo root (o "schedule apply" roda como root via systemd).
//
// As notificacoes do Omarchy sao enviadas por omarchy-notification-send, que
// fala com org.freedesktop.Notifications no barramento D-Bus da SESSAO do
// usuario. Por isso rodamos o script como o usuario (runuser), apontando para
// o barramento dele em /run/user/<uid>/bus.
package notify

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
)

// omarchySend e o script do Omarchy para enviar notificacoes.
const omarchySend = "/usr/share/omarchy/bin/omarchy-notification-send"

// steamGlyph e o icone Nerd Font (nf-md-steam) mostrado na notificacao.
const steamGlyph = "\U000F04D3"

// Send mostra (ou, com replaceID != 0, atualiza no lugar) uma notificacao na
// sessao do usuario uid e devolve o id dela, para atualizacoes futuras.
func Send(uid uint32, replaceID uint32, urgency, headline, body string) (uint32, error) {
	if _, err := os.Stat(omarchySend); err != nil {
		return 0, fmt.Errorf("omarchy-notification-send nao encontrado: %w", err)
	}
	u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return 0, fmt.Errorf("usuario %d: %w", uid, err)
	}
	runtimeDir := "/run/user/" + u.Uid
	if _, err := os.Stat(runtimeDir + "/bus"); err != nil {
		return 0, fmt.Errorf("usuario %s sem sessao D-Bus ativa: %w", u.Username, err)
	}

	args := []string{"-u", u.Username, "--",
		"env",
		"XDG_RUNTIME_DIR=" + runtimeDir,
		"DBUS_SESSION_BUS_ADDRESS=unix:path=" + runtimeDir + "/bus",
		omarchySend, "-p",
		"--app-name", "Open Turkey",
		"-g", steamGlyph,
		"-u", urgency,
		"-r", strconv.FormatUint(uint64(replaceID), 10),
		headline, body,
	}
	out, err := exec.Command("runuser", args...).Output()
	if err != nil {
		return 0, fmt.Errorf("omarchy-notification-send falhou: %w", err)
	}
	id, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("id de notificacao invalido %q", out)
	}
	return uint32(id), nil
}

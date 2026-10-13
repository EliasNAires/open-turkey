package blocker

import "testing"

func TestValidarNomeProcesso(t *testing.T) {
	validos := []string{"steam", "steamwebhelper", "discord", "firefox", "reaper"}
	for _, n := range validos {
		if err := ValidarNomeProcesso(n); err != nil {
			t.Errorf("ValidarNomeProcesso(%q) = %v, queria nil", n, err)
		}
	}

	invalidos := []string{
		"", "   ", "x", "vi", // vazio / curto
		"/usr/bin/steam", "steam web", " steam", // caminho / espacos
		"systemd", "init", "sshd", "Xorg", "gnome-shell", "kwin", "kwin_wayland",
		"sudo", "bash", "Hyprland", "hyprlock", "quickshell", "open-turkey-bin",
		"systemd-logind", "kworker/0:1",
	}
	for _, n := range invalidos {
		if err := ValidarNomeProcesso(n); err == nil {
			t.Errorf("ValidarNomeProcesso(%q) = nil, queria erro", n)
		}
	}
}

// Mesmo que um nome critico ja esteja no banco (de antes da validacao),
// KillBlocked nao deve considera-lo.
func TestFiltrarNomesSeguros(t *testing.T) {
	got := filtrarNomesSeguros([]string{"steam", "bash", "", "systemd", "steamwebhelper"})
	if len(got) != 2 || got[0] != "steam" || got[1] != "steamwebhelper" {
		t.Fatalf("filtrarNomesSeguros = %v", got)
	}
}

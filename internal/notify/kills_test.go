package notify

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/brunodcdo/open-turkey/internal/blocker"
)

func nomes(id string) string {
	if id == "1084160" {
		return "Jagged Alliance 3"
	}
	return ""
}

func TestDescribeKills(t *testing.T) {
	steam := blocker.KilledProcess{Comm: "steam", Blocked: true}
	web := blocker.KilledProcess{Comm: "steamwebhelper", Blocked: true}
	reaper := blocker.KilledProcess{Comm: "reaper", Args: []string{"/x/reaper", "SteamLaunch", "AppId=1084160", "--", "game"}}
	filho := blocker.KilledProcess{Comm: "wine64-preloade"}

	casos := []struct {
		nome        string
		killed      []blocker.KilledProcess
		head, corpo string
	}{
		{"so o cliente", []blocker.KilledProcess{steam, web, web}, "Closed Steam", "Steam is blocked right now."},
		{"cliente e jogo", []blocker.KilledProcess{steam, web, reaper, filho}, "Closed Steam and Jagged Alliance 3", "Steam is blocked right now."},
		{"jogo desconhecido", []blocker.KilledProcess{steam, {Comm: "reaper", Args: []string{"reaper", "AppId=999"}}}, "Closed Steam and your game", "Steam is blocked right now."},
		{"so o jogo (cliente ja fechado)", []blocker.KilledProcess{{Comm: "reaper", Blocked: true, Args: []string{"reaper", "AppId=1084160"}}, filho}, "Closed Jagged Alliance 3", "Steam is blocked right now."},
		{"outro app", []blocker.KilledProcess{{Comm: "discord", Blocked: true}}, "Closed Discord", "This app is blocked right now."},
	}
	for _, c := range casos {
		h, b := describeKills(c.killed, nomes)
		if h != c.head || b != c.corpo {
			t.Errorf("%s: (%q, %q), queria (%q, %q)", c.nome, h, b, c.head, c.corpo)
		}
	}
}

func TestSteamGameName(t *testing.T) {
	home := t.TempDir()
	lib2 := t.TempDir()
	root := filepath.Join(home, ".local/share/Steam/steamapps")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(lib2, "steamapps"), 0755)
	os.WriteFile(filepath.Join(root, "libraryfolders.vdf"), []byte(`"libraryfolders" { "0" { "path" "`+lib2+`" } }`), 0644)
	os.WriteFile(filepath.Join(lib2, "steamapps/appmanifest_1084160.acf"), []byte(`"AppState" { "appid" "1084160" "name" "Jagged Alliance 3" }`), 0644)

	if got := steamGameName(home, "1084160"); got != "Jagged Alliance 3" {
		t.Errorf("steamGameName = %q", got)
	}
	if got := steamGameName(home, "1"); got != "" {
		t.Errorf("app inexistente deveria dar vazio, deu %q", got)
	}
}

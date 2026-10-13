package notify

import (
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/brunodcdo/open-turkey/internal/blocker"
)

// ReportKills avisa cada usuario do que o bloqueio acabou de fechar. Nao faz
// nada se nada foi fechado. hint e uma linha extra opcional (ex: quando
// desbloqueia de novo). Erros so vao ao log: nunca atrapalham o bloqueio.
func ReportKills(killed []blocker.KilledProcess, hint string) {
	porUsuario := map[uint32][]blocker.KilledProcess{}
	for _, k := range killed {
		if k.UID != 0 {
			porUsuario[k.UID] = append(porUsuario[k.UID], k)
		}
	}
	for uid, ks := range porUsuario {
		home := ""
		if u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10)); err == nil {
			home = u.HomeDir
		}
		headline, body := describeKills(ks, func(appID string) string { return steamGameName(home, appID) })
		if hint != "" {
			body += " " + hint
		}
		if err := Show(uid, "normal", headline, body); err != nil {
			log.Printf("notify: %v", err)
		}
	}
}

// steamApps sao os processos do cliente Steam: aparecem como "Steam".
var steamApps = map[string]bool{"steam": true, "steamwebhelper": true}

var appIDRe = regexp.MustCompile(`^AppId=(\d+)$`)

// describeKills monta o texto da notificacao. gameName traduz um AppId do
// Steam no nome do jogo ("" se desconhecido). Pura, para testes.
func describeKills(killed []blocker.KilledProcess, gameName func(appID string) string) (headline, body string) {
	var apps, games []string
	seenApp, seenGame := map[string]bool{}, map[string]bool{}
	steam := false
	for _, k := range killed {
		// Jogos: o reaper do Steam traz "AppId=<id>" nos argumentos.
		if k.Comm == "reaper" {
			for _, a := range k.Args {
				if m := appIDRe.FindStringSubmatch(a); m != nil {
					name := gameName(m[1])
					if name == "" {
						name = "your game"
					}
					if !seenGame[name] {
						seenGame[name] = true
						games = append(games, name)
					}
				}
			}
			continue
		}
		if !k.Blocked {
			continue // descendentes (helpers, filhos do jogo) nao viram nome
		}
		if steamApps[k.Comm] {
			steam = true
			continue
		}
		name := displayName(k.Comm)
		if !seenApp[name] {
			seenApp[name] = true
			apps = append(apps, name)
		}
	}
	if steam {
		apps = append([]string{"Steam"}, apps...)
	}
	all := append(apps, games...)
	if len(all) == 0 {
		all = []string{"a blocked app"}
	}
	headline = "Closed " + joinAnd(all)

	what := "This app is"
	if steam || len(games) > 0 {
		what = "Steam is"
	} else if len(apps) > 1 {
		what = "These apps are"
	}
	body = what + " blocked right now."
	return headline, body
}

func joinAnd(xs []string) string {
	switch len(xs) {
	case 1:
		return xs[0]
	case 2:
		return xs[0] + " and " + xs[1]
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " and " + xs[len(xs)-1]
}

func displayName(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

var vdfPathRe = regexp.MustCompile(`"path"\s+"([^"]+)"`)
var acfNameRe = regexp.MustCompile(`"name"\s+"([^"]+)"`)

// steamGameName busca o nome do jogo no appmanifest_<id>.acf das bibliotecas
// do Steam do usuario (a padrao e as de libraryfolders.vdf).
func steamGameName(home, appID string) string {
	if home == "" {
		return ""
	}
	root := filepath.Join(home, ".local/share/Steam")
	libs := []string{root}
	if data, err := os.ReadFile(filepath.Join(root, "steamapps/libraryfolders.vdf")); err == nil {
		for _, m := range vdfPathRe.FindAllStringSubmatch(string(data), -1) {
			libs = append(libs, m[1])
		}
	}
	for _, lib := range libs {
		data, err := os.ReadFile(filepath.Join(lib, "steamapps", fmt.Sprintf("appmanifest_%s.acf", appID)))
		if err != nil {
			continue
		}
		if m := acfNameRe.FindStringSubmatch(string(data)); m != nil {
			return m[1]
		}
	}
	return ""
}

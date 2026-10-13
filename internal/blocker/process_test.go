package blocker

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestProcessMatches(t *testing.T) {
	blocked := []string{"steam", "steamwebhelper"}

	casos := []struct {
		nome  string
		comm  string
		exe   string
		argv0 string
		quer  bool
	}{
		// Deve bater
		{"comm exato", "steam", "/home/u/.local/share/Steam/ubuntu12_32/steam", "/home/u/.local/share/Steam/ubuntu12_32/steam", true},
		{"script /usr/bin/steam (comm do script, exe do bash)", "steam", "/usr/bin/bash", "/bin/bash", true},
		{"so basename do exe", "outro", "/opt/steam/steamwebhelper", "", true},
		{"so basename do argv0", "x", "", "/opt/steam/steamwebhelper", true},
		{"argv0 sem caminho", "", "", "steam", true},
		{"exe substituido em disco", "", "/opt/steam/steam (deleted)", "", true},

		// NAO deve bater
		{"vim com arquivo com steam no nome", "vim", "/usr/bin/vim", "vim", false},
		{"substring no comm", "steam.sh", "/usr/bin/bash", "/bin/bash", false},
		{"substring no exe", "steamos-tool", "/usr/bin/steamos-tool", "/usr/bin/steamos-tool", false},
		{"Proton steam.exe", "steam.exe", "/home/u/proton/bin/wine64-preloader", "C:\\windows\\system32\\steam.exe", false},
		{"diretorio com steam no caminho", "game", "/home/u/.local/share/Steam/steamapps/common/Game/game", "./game", false},
		{"maiusculas diferentes", "Steam", "", "", false},
		{"tudo vazio", "", "", "", false},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := processMatches(c.comm, c.exe, c.argv0, blocked); got != c.quer {
				t.Errorf("processMatches(%q, %q, %q) = %v, quer %v", c.comm, c.exe, c.argv0, got, c.quer)
			}
		})
	}
}

// O caso pedido explicitamente: "vim notas-steam.txt" tem "steam" no cmdline,
// mas so argv[0] e considerado — e argv[0] e "vim".
func TestProcessMatchesIgnoraArgumentos(t *testing.T) {
	argv0 := argv0FromCmdline([]byte("vim\x00notas-steam.txt\x00"))
	if argv0 != "vim" {
		t.Fatalf("argv0 = %q, quer \"vim\"", argv0)
	}
	if processMatches("vim", "/usr/bin/vim", argv0, []string{"steam"}) {
		t.Fatal("'vim notas-steam.txt' nao deveria bater com 'steam'")
	}
}

func TestProcessMatchesNomeVazioNaLista(t *testing.T) {
	if processMatches("", "", "", []string{""}) {
		t.Fatal("nome vazio na lista nunca deve bater")
	}
}

func TestBaseName(t *testing.T) {
	for in, quer := range map[string]string{
		"":                 "",
		"steam":            "steam",
		"/usr/bin/steam":   "steam",
		"/usr/bin/":        "",
		"./steamwebhelper": "steamwebhelper",
	} {
		if got := baseName(in); got != quer {
			t.Errorf("baseName(%q) = %q, quer %q", in, got, quer)
		}
	}
}

// Teste contra um processo real em /proc: um shell cujo argumento contem
// "steam" nao pode bater com "steam", mas bate pelo proprio nome.
func TestMatchesBlockedProcessReal(t *testing.T) {
	// O "; :" impede o shell de fazer exec direto no sleep.
	cmd := exec.Command("sh", "-c", "sleep 30; :", "notas-steam.txt")
	if err := cmd.Start(); err != nil {
		t.Skipf("nao consegui iniciar sh: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	pid := cmd.Process.Pid

	if matchesBlockedProcess(pid, []string{"steam"}) {
		t.Error("processo com 'notas-steam.txt' nos argumentos nao deveria bater com 'steam'")
	}
	if matchesBlockedProcess(pid, []string{"notas-steam.txt"}) {
		t.Error("argumentos alem de argv[0] nunca devem ser considerados")
	}
	if !matchesBlockedProcess(pid, []string{"sh"}) {
		t.Error("o proprio shell deveria bater com 'sh' (comm/argv0)")
	}
}

func TestFindProcessesReal(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("nao consegui iniciar sleep: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	found, err := FindProcesses([]string{"sleep"})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range found {
		if p.PID == cmd.Process.Pid {
			if p.UID != uint32(os.Getuid()) {
				t.Errorf("UID = %d, queria %d", p.UID, os.Getuid())
			}
			return
		}
	}
	t.Fatalf("sleep (pid %d) nao encontrado em %v", cmd.Process.Pid, found)
}

func TestParseStatPPID(t *testing.T) {
	casos := map[string]int{
		"1234 (steam) S 1000 1234 1234 0 -1":   1000,
		"77 (Web Content (x)) S 42 77 77 0 -1": 42,
		"9 (a) b) R 5 9 9 0":                   5,
	}
	for stat, quer := range casos {
		if got, ok := parseStatPPID(stat); !ok || got != quer {
			t.Errorf("parseStatPPID(%q) = %d,%v; queria %d", stat, got, ok, quer)
		}
	}
	if _, ok := parseStatPPID("lixo"); ok {
		t.Error("stat invalido deveria falhar")
	}
}

func TestWithDescendants(t *testing.T) {
	// steam(10, uid 1000) → reaper(11) → jogo(12) → wine(13)
	//                     → pkexec helper(14, uid 0) → filho(15, uid 0)
	// outro processo(20) nao relacionado
	procs := map[int]procNode{
		1:  {PPID: 0, UID: 0},
		10: {PPID: 1, UID: 1000},
		11: {PPID: 10, UID: 1000},
		12: {PPID: 11, UID: 1000},
		13: {PPID: 12, UID: 1000},
		14: {PPID: 10, UID: 0},
		15: {PPID: 14, UID: 0},
		20: {PPID: 1, UID: 1000},
	}
	got := map[int]bool{}
	for _, p := range withDescendants([]int{10}, procs) {
		got[p] = true
	}
	for _, p := range []int{10, 11, 12, 13} {
		if !got[p] {
			t.Errorf("pid %d deveria ser morto", p)
		}
	}
	for _, p := range []int{1, 14, 15, 20} {
		if got[p] {
			t.Errorf("pid %d NAO deveria ser morto", p)
		}
	}
}

// Teste real: matar um "pai" bloqueado tem que levar junto o filho.
func TestKillBlockedMataDescendentesReal(t *testing.T) {
	dir := t.TempDir()
	// Um shell renomeado "otpaiteste" que fica esperando um sleep filho.
	bin := dir + "/otpaiteste"
	data, err := os.ReadFile("/bin/sh")
	if err != nil {
		t.Skip(err)
	}
	if err := os.WriteFile(bin, data, 0755); err != nil {
		t.Skip(err)
	}
	pai := exec.Command(bin, "-c", "sleep 30 & echo $!; wait")
	out, err := pai.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := pai.Start(); err != nil {
		t.Skip(err)
	}
	defer func() { _ = pai.Process.Kill(); _ = pai.Wait() }()
	var filho int
	if _, err := fmt.Fscan(out, &filho); err != nil {
		t.Fatal(err)
	}

	if _, err := KillBlocked([]string{"otpaiteste"}); err != nil {
		t.Fatal(err)
	}
	_ = pai.Wait()
	// O filho vira zumbi ou some; em ambos os casos nao esta mais rodando.
	time.Sleep(100 * time.Millisecond)
	if st, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", filho)); err == nil && !strings.Contains(string(st), ") Z ") {
		t.Errorf("filho %d (sleep) deveria ter morrido junto com o pai: %s", filho, st)
	}
}

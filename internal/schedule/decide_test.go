package schedule

import (
	"testing"
	"time"
)

func graceConfig(t *testing.T) *Config {
	t.Helper()
	cfg := loadRepoConfig(t)
	cfg.Grace = 15 * time.Minute
	cfg.GameProcesses = []string{"reaper"}
	return cfg
}

// Segunda 12:30 comeca uma franja bloqueada (fim da janela 11:30-12:30).
func TestDecideSemJogoAtivaNaHora(t *testing.T) {
	cfg := graceConfig(t)
	d := cfg.Decide(at(t, cfg, "2026-10-12 12:30:00"), false, false, false, State{})
	if !d.Activate || d.Notice != nil {
		t.Fatalf("sem jogo deveria ativar sem aviso: %+v", d)
	}
	if !d.State.GraceFor.Equal(at(t, cfg, "2026-10-12 12:30:00")) {
		t.Errorf("franja marcada errada: %v", d.State.GraceFor)
	}
}

func TestDecideCarenciaCompleta(t *testing.T) {
	cfg := graceConfig(t)
	inicio := at(t, cfg, "2026-10-12 12:30:00")

	// 12:30 com jogo: nao ativa, avisa "15 min".
	d := cfg.Decide(inicio, false, false, true, State{})
	if d.Activate || d.Notice == nil || d.Notice.Headline != "Steam will be blocked in 15 min" {
		t.Fatalf("12:30: %+v", d)
	}
	st := d.State

	// 12:40: ainda em carencia, faltam 5 min; mantem o id da notificacao.
	d = cfg.Decide(at(t, cfg, "2026-10-12 12:40:00"), false, false, true, st)
	if d.Activate || d.Notice == nil || d.Notice.Headline != "Steam will be blocked in 5 min" {
		t.Fatalf("12:40: %+v", d)
	}

	// 12:45: acabou o prazo, ativa mesmo com jogo. Sem aviso proprio: o
	// aviso de "Closed ..." vem de quem mata os processos.
	d = cfg.Decide(at(t, cfg, "2026-10-12 12:45:00"), false, false, true, d.State)
	if !d.Activate || d.Notice != nil {
		t.Fatalf("12:45: %+v", d)
	}
}

func TestDecideJogoFechaDuranteCarencia(t *testing.T) {
	cfg := graceConfig(t)
	d := cfg.Decide(at(t, cfg, "2026-10-12 12:30:00"), false, false, true, State{})
	d = cfg.Decide(at(t, cfg, "2026-10-12 12:33:00"), false, false, false, d.State)
	if !d.Activate || d.Notice == nil || d.Notice.Body != "Game closed: block is now active." {
		t.Fatalf("jogo fechado deveria ativar com aviso: %+v", d)
	}
}

// Depois de ativado, "stop" manual + abrir jogo nao pode render outra carencia.
func TestDecideCarenciaNaoRenova(t *testing.T) {
	cfg := graceConfig(t)
	d := cfg.Decide(at(t, cfg, "2026-10-12 12:30:00"), false, false, false, State{})
	d = cfg.Decide(at(t, cfg, "2026-10-12 12:31:00"), true, false, false, d.State) // ja ativo
	// usuario fez "stop" e abriu um jogo
	d = cfg.Decide(at(t, cfg, "2026-10-12 13:00:00"), false, false, true, d.State)
	if !d.Activate || d.Notice != nil {
		t.Fatalf("carencia nao deveria renovar na mesma franja: %+v", d)
	}
}

// A franja seguinte (18:00) tem direito a nova carencia.
func TestDecideNovaFranjaNovaCarencia(t *testing.T) {
	cfg := graceConfig(t)
	d := cfg.Decide(at(t, cfg, "2026-10-12 12:30:00"), false, false, false, State{})
	d = cfg.Decide(at(t, cfg, "2026-10-12 17:30:00"), true, false, false, d.State) // janela livre
	if !d.Deactivate {
		t.Fatalf("17:30 deveria desativar: %+v", d)
	}
	d = cfg.Decide(at(t, cfg, "2026-10-12 18:00:00"), false, false, true, d.State)
	if d.Activate || d.Notice == nil {
		t.Fatalf("18:00 com jogo deveria abrir carencia nova: %+v", d)
	}
}

func TestDecideSemCarenciaConfigurada(t *testing.T) {
	cfg := loadRepoConfig(t)
	cfg.Grace = 0
	d := cfg.Decide(at(t, cfg, "2026-10-12 12:30:00"), false, false, true, State{})
	if !d.Activate {
		t.Fatalf("sem grace_minutes deveria ativar mesmo com jogo: %+v", d)
	}
}

func TestDecideJanelaLivreETravado(t *testing.T) {
	cfg := graceConfig(t)
	livre := at(t, cfg, "2026-10-12 11:45:00")
	if d := cfg.Decide(livre, true, true, false, State{}); d.Deactivate {
		t.Error("bloco travado (--lock) nunca deve ser desativado pelo horario")
	}
	if d := cfg.Decide(livre, true, false, false, State{}); !d.Deactivate {
		t.Error("janela livre deveria desativar")
	}
	if d := cfg.Decide(livre, false, false, false, State{}); d.Activate || d.Deactivate {
		t.Error("janela livre e inativo: nada a fazer")
	}
}

func TestPeriodStart(t *testing.T) {
	cfg := loadRepoConfig(t)
	casos := map[string]string{
		"2026-10-12 13:10:30": "2026-10-12 12:30:00",
		"2026-10-12 12:30:00": "2026-10-12 12:30:00",
		"2026-10-12 05:00:00": "2026-10-12 00:00:00", // segunda de madrugada: desde 00:00
		"2026-10-17 10:00:00": "2026-10-16 20:00:00", // sabado: livre desde sexta 20:00
	}
	for de, quer := range casos {
		if got := cfg.PeriodStart(at(t, cfg, de)); !got.Equal(at(t, cfg, quer)) {
			t.Errorf("PeriodStart(%s) = %v, queria %s", de, got, quer)
		}
	}
}

func TestParseCarencia(t *testing.T) {
	if _, err := Parse([]byte(`{"block":"x","timezone":"UTC","grace_minutes":10}`)); err == nil {
		t.Error("grace_minutes sem game_processes deveria falhar")
	}
	if _, err := Parse([]byte(`{"block":"x","timezone":"UTC","grace_minutes":-1,"game_processes":["reaper"]}`)); err == nil {
		t.Error("grace_minutes negativo deveria falhar")
	}
	cfg, err := Parse([]byte(`{"block":"x","timezone":"UTC","grace_minutes":10,"game_processes":["reaper"]}`))
	if err != nil || cfg.Grace != 10*time.Minute {
		t.Errorf("Parse valido: %v %v", cfg, err)
	}
}

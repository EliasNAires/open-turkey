package schedule

import (
	"os"
	"testing"
	"time"
)

// loadRepoConfig carrega o horario real versionado em etc/schedule.json.
func loadRepoConfig(t *testing.T) *Config {
	t.Helper()
	cfg, err := Load("../../etc/schedule.json")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// at monta um instante em Buenos Aires. 2026-10-12 e uma segunda-feira.
func at(t *testing.T, cfg *Config, s string) time.Time {
	t.Helper()
	tm, err := time.ParseInLocation("2006-01-02 15:04:05", s, cfg.Location)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func TestShouldBlockHorarioReal(t *testing.T) {
	cfg := loadRepoConfig(t)
	casos := []struct {
		quando    string
		bloqueado bool
	}{
		// domingo -> segunda: o bloqueio comeca as 00:00 de segunda
		{"2026-10-11 23:59:59", false},
		{"2026-10-12 00:00:00", true},
		// segunda: 11:30-12:30, 17:30-18:00
		{"2026-10-12 11:29:59", true},
		{"2026-10-12 11:30:00", false},
		{"2026-10-12 12:29:59", false},
		{"2026-10-12 12:30:00", true},
		{"2026-10-12 14:30:00", true}, // janela de terca, nao de segunda
		{"2026-10-12 17:30:00", false},
		{"2026-10-12 18:00:00", true},
		{"2026-10-12 20:00:00", true},
		// terca: 14:30-15:30
		{"2026-10-13 11:30:00", true},
		{"2026-10-13 14:30:00", false},
		{"2026-10-13 15:30:00", true},
		// quarta: 20:00-24:00, e quinta 00:00 volta a bloquear
		{"2026-10-14 19:59:59", true},
		{"2026-10-14 20:00:00", false},
		{"2026-10-14 23:59:59", false},
		{"2026-10-15 00:00:00", true},
		// sexta 20:00 em diante e o fim de semana inteiro: livre
		{"2026-10-16 19:59:00", true},
		{"2026-10-16 20:00:00", false},
		{"2026-10-16 23:59:59", false},
		{"2026-10-17 00:00:00", false},
		{"2026-10-17 15:00:00", false},
		{"2026-10-18 23:59:59", false},
		{"2026-10-19 00:00:00", true},
	}
	for _, c := range casos {
		tm := at(t, cfg, c.quando)
		if got := cfg.ShouldBlock(tm); got != c.bloqueado {
			t.Errorf("%s (%s): bloqueado=%v, queria %v", c.quando, tm.Weekday(), got, c.bloqueado)
		}
	}
}

// O fuso do arquivo vale mesmo que a hora venha em outro fuso (ex: UTC).
func TestShouldBlockConverteFuso(t *testing.T) {
	cfg := loadRepoConfig(t)
	// 14:30 UTC = 11:30 em Buenos Aires (UTC-3), segunda -> liberado
	if cfg.ShouldBlock(time.Date(2026, 10, 12, 14, 30, 0, 0, time.UTC)) {
		t.Error("14:30 UTC de segunda deveria ser 11:30 local (liberado)")
	}
}

func TestNextChange(t *testing.T) {
	cfg := loadRepoConfig(t)
	casos := []struct{ de, quer string }{
		{"2026-10-12 09:00:00", "2026-10-12 11:30:00"},
		{"2026-10-12 11:45:10", "2026-10-12 12:30:00"},
		{"2026-10-16 21:00:00", "2026-10-19 00:00:00"}, // sexta a noite -> segunda 00:00
	}
	for _, c := range casos {
		got, ok := cfg.NextChange(at(t, cfg, c.de))
		if !ok || !got.Equal(at(t, cfg, c.quer)) {
			t.Errorf("NextChange(%s) = %v (ok=%v), queria %s", c.de, got, ok, c.quer)
		}
	}
}

func TestNextChangeNuncaMuda(t *testing.T) {
	cfg, err := Parse([]byte(`{"block":"x","timezone":"UTC","unblocked":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.NextChange(time.Now()); ok {
		t.Error("sem janelas o estado nunca muda")
	}
}

func TestParseRejeita(t *testing.T) {
	invalidos := map[string]string{
		"campo desconhecido": `{"block":"x","timezone":"UTC","unblock":{}}`,
		"sem bloco":          `{"timezone":"UTC"}`,
		"sem fuso":           `{"block":"x"}`,
		"fuso invalido":      `{"block":"x","timezone":"Marte/Olimpo"}`,
		"dia invalido":       `{"block":"x","timezone":"UTC","unblocked":{"lunes":["10:00-11:00"]}}`,
		"sem hifen":          `{"block":"x","timezone":"UTC","unblocked":{"monday":["10:00"]}}`,
		"fim antes":          `{"block":"x","timezone":"UTC","unblocked":{"monday":["12:00-11:00"]}}`,
		"cruza meia-noite":   `{"block":"x","timezone":"UTC","unblocked":{"monday":["23:00-01:00"]}}`,
		"vazia":              `{"block":"x","timezone":"UTC","unblocked":{"monday":["10:00-10:00"]}}`,
		"minuto 60":          `{"block":"x","timezone":"UTC","unblocked":{"monday":["10:60-11:00"]}}`,
		"24:30":              `{"block":"x","timezone":"UTC","unblocked":{"monday":["10:00-24:30"]}}`,
		"inicio 24:00":       `{"block":"x","timezone":"UTC","unblocked":{"monday":["24:00-24:00"]}}`,
		"formato curto":      `{"block":"x","timezone":"UTC","unblocked":{"monday":["9:00-10:00"]}}`,
	}
	for nome, js := range invalidos {
		if _, err := Parse([]byte(js)); err == nil {
			t.Errorf("%s: Parse deveria falhar", nome)
		}
	}
}

func TestLoadArquivoAusente(t *testing.T) {
	if _, err := Load(t.TempDir() + "/nao-existe.json"); !os.IsNotExist(unwrap(err)) {
		t.Errorf("esperava erro de arquivo inexistente, veio %v", err)
	}
}

func unwrap(err error) error {
	for {
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return err
		}
		err = u.Unwrap()
	}
}

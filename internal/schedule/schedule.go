// Package schedule implementa horarios semanais para um bloco do Open Turkey.
//
// O horario descreve JANELAS DE DESBLOQUEIO: dentro de uma janela o bloco fica
// desativado; fora de todas as janelas, ativado. Um dia sem entrada no arquivo
// fica bloqueado o dia inteiro; um dia livre usa a janela "00:00-24:00".
//
// Exemplo de /etc/open-turkey/schedule.json:
//
//	{
//	  "block": "steam",
//	  "timezone": "America/Argentina/Buenos_Aires",
//	  "unblocked": {
//	    "monday":   ["11:30-12:30", "17:30-18:00"],
//	    "saturday": ["00:00-24:00"]
//	  }
//	}
//
// A decisao (ShouldBlock) e uma funcao pura da hora recebida, para que o
// estado seja sempre recalculado a partir do relogio — e nao do ultimo evento
// que rodou — depois de um boot ou de voltar da suspensao.
package schedule

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	// Embute a base de fusos horarios no binario, para nao depender do
	// /usr/share/zoneinfo do sistema.
	_ "time/tzdata"
)

// DefaultConfigPath e onde o instalador coloca o arquivo de horario.
const DefaultConfigPath = "/etc/open-turkey/schedule.json"

const minutesPerDay = 24 * 60

// Window e uma janela de desbloqueio [Start, End) em minutos desde 00:00.
// End pode ser 1440 ("24:00"), o fim do dia.
type Window struct {
	Start, End int
}

// Config e o horario ja validado.
type Config struct {
	Block    string
	Location *time.Location
	// Unblocked[time.Weekday] lista as janelas de desbloqueio daquele dia.
	Unblocked [7][]Window
}

// fileConfig e o formato do arquivo JSON.
type fileConfig struct {
	Comment   string              `json:"_comment,omitempty"`
	Block     string              `json:"block"`
	Timezone  string              `json:"timezone"`
	Unblocked map[string][]string `json:"unblocked"`
}

var weekdays = map[string]time.Weekday{
	"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday,
	"wednesday": time.Wednesday, "thursday": time.Thursday,
	"friday": time.Friday, "saturday": time.Saturday,
}

// Load le e valida o arquivo de horario.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("erro ao ler horario '%s': %w", path, err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("horario '%s' invalido: %w", path, err)
	}
	return cfg, nil
}

// Parse valida o conteudo JSON de um horario. Campos desconhecidos sao erro,
// para que um erro de digitacao (ex: "unblock") nao vire "bloqueado sempre".
func Parse(data []byte) (*Config, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var fc fileConfig
	if err := dec.Decode(&fc); err != nil {
		return nil, err
	}
	if strings.TrimSpace(fc.Block) == "" {
		return nil, fmt.Errorf("campo 'block' vazio")
	}
	if fc.Timezone == "" {
		return nil, fmt.Errorf("campo 'timezone' vazio")
	}
	loc, err := time.LoadLocation(fc.Timezone)
	if err != nil {
		return nil, fmt.Errorf("timezone '%s' invalido: %w", fc.Timezone, err)
	}

	cfg := &Config{Block: fc.Block, Location: loc}
	for day, windows := range fc.Unblocked {
		wd, ok := weekdays[strings.ToLower(day)]
		if !ok {
			return nil, fmt.Errorf("dia '%s' invalido (use monday..sunday)", day)
		}
		for _, w := range windows {
			win, err := parseWindow(w)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", day, err)
			}
			cfg.Unblocked[wd] = append(cfg.Unblocked[wd], win)
		}
	}
	return cfg, nil
}

// parseWindow le "HH:MM-HH:MM". "24:00" so e aceito como fim.
func parseWindow(s string) (Window, error) {
	a, b, ok := strings.Cut(strings.TrimSpace(s), "-")
	if !ok {
		return Window{}, fmt.Errorf("janela '%s' invalida (use HH:MM-HH:MM)", s)
	}
	start, err := parseClock(a)
	if err != nil {
		return Window{}, fmt.Errorf("janela '%s': %w", s, err)
	}
	end, err := parseClock(b)
	if err != nil {
		return Window{}, fmt.Errorf("janela '%s': %w", s, err)
	}
	if start == minutesPerDay {
		return Window{}, fmt.Errorf("janela '%s': 24:00 so pode ser o fim", s)
	}
	if end <= start {
		return Window{}, fmt.Errorf("janela '%s': o fim deve ser depois do inicio (janelas nao cruzam a meia-noite; divida em dois dias)", s)
	}
	return Window{Start: start, End: end}, nil
}

// parseClock le "HH:MM" e devolve minutos desde 00:00 (0..1440).
func parseClock(s string) (int, error) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok || len(h) != 2 || len(m) != 2 {
		return 0, fmt.Errorf("hora '%s' invalida (use HH:MM)", s)
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || mm < 0 || mm > 59 || hh > 24 || (hh == 24 && mm != 0) {
		return 0, fmt.Errorf("hora '%s' invalida", s)
	}
	return hh*60 + mm, nil
}

// ShouldBlock diz se o bloco deve estar ativo no instante t (convertido para
// o fuso do horario). Janelas sao [inicio, fim): as 11:30 de "11:30-12:30"
// ja esta liberado; as 12:30, bloqueado de novo.
func (c *Config) ShouldBlock(t time.Time) bool {
	local := t.In(c.Location)
	minute := local.Hour()*60 + local.Minute()
	for _, w := range c.Unblocked[local.Weekday()] {
		if minute >= w.Start && minute < w.End {
			return false
		}
	}
	return true
}

// NextChange devolve o proximo instante (alinhado ao minuto) em que
// ShouldBlock muda de valor depois de t, procurando ate 8 dias a frente.
// ok=false se o estado nunca muda (ex: bloqueado a semana toda).
func (c *Config) NextChange(t time.Time) (next time.Time, ok bool) {
	current := c.ShouldBlock(t)
	m := t.Truncate(time.Minute)
	for i := 0; i < 8*minutesPerDay; i++ {
		m = m.Add(time.Minute)
		if c.ShouldBlock(m) != current {
			return m, true
		}
	}
	return time.Time{}, false
}

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
//	  },
//	  "exceptions": [
//	    {"from": "2026-10-13 20:30", "to": "2026-10-13 22:30", "reason": "a aula acabou mais cedo"}
//	  ]
//	}
//
// Excecoes sao janelas de desbloqueio AVULSAS, com data e hora absolutas: valem
// uma unica vez e vencem sozinhas (ficam no arquivo como historico). Para nao
// virarem um atalho, exigem um motivo e tem limites (MaxExceptionDuration e
// MaxExceptionsPerWeek); as que passam do limite sao ignoradas, sem invalidar
// o resto do horario.
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

// Limites das excecoes avulsas. Ficam no codigo, e nao no arquivo, para que
// afrouxa-los exija recompilar e reinstalar — e nao so editar o JSON.
const (
	MaxExceptionDuration = 2 * time.Hour
	MaxExceptionsPerWeek = 2
)

// ExceptionLayout e o formato de data/hora das excecoes, no fuso do horario.
const ExceptionLayout = "2006-01-02 15:04"

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
	// Grace e o tempo maximo de espera, quando o bloqueio comeca com um jogo
	// aberto, antes de ativar o bloco mesmo assim. Zero desativa a carencia.
	Grace time.Duration
	// GameProcesses sao os nomes (exatos) que indicam um jogo rodando.
	GameProcesses []string
	// Exceptions sao as excecoes avulsas validas (dentro dos limites).
	Exceptions []Exception
	// Ignored explica cada excecao descartada por passar dos limites.
	Ignored []string
}

// Exception e uma janela de desbloqueio avulsa [From, To).
type Exception struct {
	From, To time.Time
	Reason   string
}

// fileConfig e o formato do arquivo JSON.
type fileConfig struct {
	Comment       string              `json:"_comment,omitempty"`
	Block         string              `json:"block"`
	Timezone      string              `json:"timezone"`
	Unblocked     map[string][]string `json:"unblocked"`
	GraceMinutes  int                 `json:"grace_minutes,omitempty"`
	GameProcesses []string            `json:"game_processes,omitempty"`
	Exceptions    []fileException     `json:"exceptions,omitempty"`
}

type fileException struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
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

	if fc.GraceMinutes < 0 || fc.GraceMinutes > 180 {
		return nil, fmt.Errorf("grace_minutes deve estar entre 0 e 180")
	}
	if fc.GraceMinutes > 0 && len(fc.GameProcesses) == 0 {
		return nil, fmt.Errorf("grace_minutes exige game_processes (ex: [\"reaper\"])")
	}
	for _, p := range fc.GameProcesses {
		if strings.TrimSpace(p) == "" || strings.ContainsAny(p, " /") {
			return nil, fmt.Errorf("game_processes: nome '%s' invalido", p)
		}
	}

	cfg := &Config{
		Block:         fc.Block,
		Location:      loc,
		Grace:         time.Duration(fc.GraceMinutes) * time.Minute,
		GameProcesses: fc.GameProcesses,
	}
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
	if err := cfg.parseExceptions(fc.Exceptions); err != nil {
		return nil, err
	}
	return cfg, nil
}

// parseExceptions valida as excecoes. Formato errado e erro (como nas janelas);
// passar dos limites so descarta a excecao, registrando o motivo em Ignored.
func (c *Config) parseExceptions(list []fileException) error {
	perWeek := map[time.Time]int{}
	for i, fe := range list {
		from, err := time.ParseInLocation(ExceptionLayout, strings.TrimSpace(fe.From), c.Location)
		if err != nil {
			return fmt.Errorf("exceptions[%d]: 'from' invalido (use \"AAAA-MM-DD HH:MM\"): %w", i, err)
		}
		to, err := time.ParseInLocation(ExceptionLayout, strings.TrimSpace(fe.To), c.Location)
		if err != nil {
			return fmt.Errorf("exceptions[%d]: 'to' invalido (use \"AAAA-MM-DD HH:MM\"): %w", i, err)
		}
		if !to.After(from) {
			return fmt.Errorf("exceptions[%d]: 'to' deve ser depois de 'from'", i)
		}
		reason := strings.TrimSpace(fe.Reason)
		if reason == "" {
			return fmt.Errorf("exceptions[%d]: 'reason' e obrigatorio", i)
		}

		label := fmt.Sprintf("%s -> %s (%s)", fe.From, fe.To, reason)
		if to.Sub(from) > MaxExceptionDuration {
			c.Ignored = append(c.Ignored, fmt.Sprintf("%s: passa de %s", label, MaxExceptionDuration))
			continue
		}
		week := weekStart(from)
		if perWeek[week] >= MaxExceptionsPerWeek {
			c.Ignored = append(c.Ignored, fmt.Sprintf("%s: ja ha %d excecoes na semana de %s", label, MaxExceptionsPerWeek, week.Format("2006-01-02")))
			continue
		}
		perWeek[week]++
		c.Exceptions = append(c.Exceptions, Exception{From: from, To: to, Reason: reason})
	}
	return nil
}

// weekStart devolve a segunda-feira 00:00 da semana de t (no fuso de t).
func weekStart(t time.Time) time.Time {
	y, m, d := t.Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
}

// ActiveException devolve a excecao em vigor no instante t, se houver.
func (c *Config) ActiveException(t time.Time) (Exception, bool) {
	for _, e := range c.Exceptions {
		if !t.Before(e.From) && t.Before(e.To) {
			return e, true
		}
	}
	return Exception{}, false
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
// ja esta liberado; as 12:30, bloqueado de novo. Uma excecao em vigor tambem
// libera.
func (c *Config) ShouldBlock(t time.Time) bool {
	if _, ok := c.ActiveException(t); ok {
		return false
	}
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

// PeriodStart devolve o inicio (alinhado ao minuto) do trecho continuo de
// mesmo estado que contem t, procurando ate 8 dias para tras. Serve para
// identificar "esta franja bloqueada" de forma estavel entre execucoes.
// Se o estado nunca muda, devolve o instante zero.
func (c *Config) PeriodStart(t time.Time) time.Time {
	current := c.ShouldBlock(t)
	m := t.Truncate(time.Minute)
	for i := 0; i < 8*minutesPerDay; i++ {
		prev := m.Add(-time.Minute)
		if c.ShouldBlock(prev) != current {
			return m
		}
		m = prev
	}
	return time.Time{}
}

// UnblockHint devolve "Unblocked again: Wed 11:30." se o horario instalado
// estiver bloqueando agora, ou "" se nao houver horario/estado aplicavel.
func UnblockHint(now time.Time) string {
	cfg, err := Load(DefaultConfigPath)
	if err != nil || !cfg.ShouldBlock(now) {
		return ""
	}
	next, ok := cfg.NextChange(now)
	if !ok {
		return ""
	}
	return "Unblocked again: " + next.In(cfg.Location).Format("Mon 15:04") + "."
}

package schedule

import (
	"fmt"
	"strings"
	"time"
)

// State e o que "schedule apply" guarda entre execucoes (em
// /var/lib/open-turkey/schedule-state.json) para a carencia de jogos.
type State struct {
	// GraceFor e o inicio da franja bloqueada em que a carencia ja foi
	// usada (ou em que o bloco ja foi ativado). Cada franja da no maximo
	// UMA carencia: desativar o bloco e abrir um jogo nao renova o prazo.
	GraceFor time.Time `json:"grace_for"`
	// GraceStart e quando a carencia atual comecou; zero = sem carencia.
	GraceStart time.Time `json:"grace_start"`
	// NotifyID e o id da notificacao, para atualiza-la no lugar.
	NotifyID uint32 `json:"notify_id,omitempty"`
}

// Notice e uma notificacao a mostrar ao usuario.
type Notice struct {
	Headline, Body string
	Urgency        string // low | normal | critical
}

// Decision e o que apply deve fazer agora.
type Decision struct {
	Activate, Deactivate bool
	State                State
	Notice               *Notice
}

// Decide e a regra completa de "schedule apply", pura (sem banco, /proc nem
// relogio): dado o instante, o estado do bloco e se ha jogo rodando, diz se
// ativa/desativa, o novo State e se mostra uma notificacao.
func (c *Config) Decide(now time.Time, active, locked, gameRunning bool, st State) Decision {
	if !c.ShouldBlock(now) {
		// Janela livre: a carencia de uma franja anterior nao vale mais.
		return Decision{Deactivate: active && !locked, State: State{}}
	}

	period := c.PeriodStart(now)
	if active {
		// Ja bloqueado (pelo horario ou manualmente): marca a franja como
		// usada, para que um "stop" manual + jogo nao ganhe carencia nova.
		return Decision{State: State{GraceFor: period}}
	}

	inGrace := !st.GraceStart.IsZero() && st.GraceFor.Equal(period)
	graceAvailable := !st.GraceFor.Equal(period)

	if c.Grace > 0 && gameRunning && (inGrace || graceAvailable) {
		if !inGrace {
			st = State{GraceFor: period, GraceStart: now, NotifyID: st.NotifyID}
		}
		remaining := st.GraceStart.Add(c.Grace).Sub(now)
		if remaining > 0 {
			mins := int((remaining + time.Minute - 1) / time.Minute) // arredonda para cima
			return Decision{State: st, Notice: &Notice{
				Headline: fmt.Sprintf("%s will be blocked in %d min", displayName(c.Block), mins),
				Body:     "Blocked hours have started. Save and quit your game: it will be blocked when you close it or when time runs out.",
				Urgency:  "critical",
			}}
		}
		return Decision{Activate: true, State: State{GraceFor: period, NotifyID: st.NotifyID}, Notice: &Notice{
			Headline: fmt.Sprintf("%s blocked", displayName(c.Block)),
			Body:     "Grace period is over.",
			Urgency:  "normal",
		}}
	}

	d := Decision{Activate: true, State: State{GraceFor: period, NotifyID: st.NotifyID}}
	if inGrace {
		// O jogo fechou antes do fim da carencia.
		d.Notice = &Notice{Headline: fmt.Sprintf("%s blocked", displayName(c.Block)), Body: "Game closed: block is now active.", Urgency: "normal"}
	}
	return d
}

// displayName capitaliza o nome do bloco para a notificacao ("steam" -> "Steam").
func displayName(block string) string {
	if block == "" {
		return block
	}
	return strings.ToUpper(block[:1]) + block[1:]
}

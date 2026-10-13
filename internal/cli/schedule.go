// Comandos "schedule": aplicam o horario semanal de /etc/open-turkey/schedule.json
// a um bloco. O estado e sempre recalculado a partir da hora atual, entao
// "schedule apply" pode rodar a qualquer momento (boot, volta da suspensao,
// timer de minuto em minuto) e e idempotente: so mexe no bloco se o estado
// atual diferir do horario.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"syscall"
	"time"

	"github.com/brunodcdo/open-turkey/internal/blocker"
	"github.com/brunodcdo/open-turkey/internal/notify"
	"github.com/brunodcdo/open-turkey/internal/schedule"
	"github.com/spf13/cobra"
)

var scheduleCmd = &cobra.Command{
	Use:   "schedule",
	Short: "Aplicar o horario semanal de um bloco",
}

var scheduleApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Ativar/desativar o bloco conforme o horario neste momento",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Imprimimos o erro nos mesmos com prioridade "err" (<3>) e saimos:
		// o Execute() o imprimiria como info, que o LogLevelMax=notice da
		// unidade descarta.
		cfg, err := loadScheduleConfig(cmd)
		if err == nil {
			for _, ig := range cfg.Ignored {
				logPrio(4, "horario: excecao ignorada: %s", ig)
			}
			err = applySchedule(cfg, time.Now())
		}
		if err != nil {
			logPrio(3, "horario: erro: %v", err)
			os.Exit(1)
		}
		return nil
	},
}

var scheduleStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Mostrar o estado que o horario exige (agora ou em --at)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadScheduleConfig(cmd)
		if err != nil {
			return err
		}

		quando := time.Now()
		atStr, _ := cmd.Flags().GetString("at")
		if atStr != "" {
			quando, err = time.ParseInLocation("2006-01-02 15:04", atStr, cfg.Location)
			if err != nil {
				return fmt.Errorf("--at invalido (use \"AAAA-MM-DD HH:MM\"): %w", err)
			}
		}

		local := quando.In(cfg.Location)
		fmt.Printf("Bloco:    %s\n", cfg.Block)
		fmt.Printf("Hora:     %s (%s)\n", local.Format("2006-01-02 15:04"), weekdayPT(local.Weekday()))
		fmt.Printf("Horario:  %s\n", estadoTexto(cfg.ShouldBlock(quando)))
		if e, ok := cfg.ActiveException(quando); ok {
			fmt.Printf("Excecao:  ate %s — %s\n", e.To.In(cfg.Location).Format("15:04"), e.Reason)
		}
		if next, ok := cfg.NextChange(quando); ok {
			n := next.In(cfg.Location)
			fmt.Printf("Proxima mudanca: %s %s -> %s\n", weekdayPT(n.Weekday()), n.Format("2006-01-02 15:04"), estadoTexto(cfg.ShouldBlock(next)))
		}
		for _, e := range cfg.Exceptions {
			if e.From.After(quando) {
				fmt.Printf("Excecao futura: %s -> %s — %s\n", e.From.In(cfg.Location).Format(schedule.ExceptionLayout), e.To.In(cfg.Location).Format("15:04"), e.Reason)
			}
		}
		for _, ig := range cfg.Ignored {
			fmt.Printf("Excecao IGNORADA: %s\n", ig)
		}

		// O estado real so faz sentido para "agora".
		if atStr == "" {
			database, err := openDB()
			if err != nil {
				return err
			}
			defer database.Close()
			detalhe, err := database.GetBlock(cfg.Block)
			if err != nil {
				return err
			}
			fmt.Printf("Real:     %s\n", estadoTexto(detalhe.Active))
		}
		return nil
	},
}

// scheduleStatePath guarda o State da carencia entre execucoes de apply.
const scheduleStatePath = "/var/lib/open-turkey/schedule-state.json"

// applySchedule leva o bloco ao estado que o horario exige no instante agora.
// A politica (incluindo a carencia para jogos abertos) esta em
// schedule.Config.Decide; aqui so ficam os efeitos: banco, camadas,
// notificacoes e o arquivo de estado.
func applySchedule(cfg *schedule.Config, agora time.Time) error {
	database, err := openDB()
	if err != nil {
		return err
	}
	defer database.Close()

	detalhe, err := database.GetBlock(cfg.Block)
	if err != nil {
		return err
	}

	// So procuramos jogos quando a carencia pode importar.
	var jogos []blocker.ProcessInfo
	if cfg.Grace > 0 && cfg.ShouldBlock(agora) && !detalhe.Active {
		jogos, err = blocker.FindProcesses(cfg.GameProcesses)
		if err != nil {
			logPrio(4, "horario: erro ao procurar jogos: %v", err)
		}
	}

	st := loadScheduleState()
	d := cfg.Decide(agora, detalhe.Active, detalhe.Locked, len(jogos) > 0, st)

	if d.Notice != nil {
		notifyOwners(jogos, d.Notice)
		logNotice("horario: %s — %s", d.Notice.Headline, d.Notice.Body)
	}

	switch {
	case d.Activate:
		if len(detalhe.Sites) == 0 && len(detalhe.Apps) == 0 {
			return fmt.Errorf("bloco '%s' nao tem sites nem apps", cfg.Block)
		}
		// Nunca travamos (--lock): um timer nao consegue resolver o desafio
		// de desbloqueio depois.
		if err := database.ActivateBlock(cfg.Block, false, 0); err != nil {
			return err
		}
		if err := reaplicarOuRemoverCamadas(database); err != nil {
			return err
		}
		logNotice("horario: bloco '%s' ATIVADO", cfg.Block)
	case d.Deactivate:
		if err := database.DeactivateBlock(cfg.Block); err != nil {
			return err
		}
		if err := reaplicarOuRemoverCamadas(database); err != nil {
			return err
		}
		logNotice("horario: bloco '%s' DESATIVADO", cfg.Block)
	}
	// Um bloco travado manualmente com --lock nunca e desativado aqui (Decide
	// nao pede): so o desafio de "unlock" pode desativa-lo.

	if d.State != st {
		saveScheduleState(d.State)
	}
	return nil
}

// notifyOwners mostra a notificacao para os donos dos processos de jogo
// (normalmente um so usuario), ou, sem jogos (ex: o jogo fechou), para o unico
// usuario com sessao. Erros nao impedem o bloqueio: so vao ao log.
func notifyOwners(jogos []blocker.ProcessInfo, n *schedule.Notice) {
	vistos := map[uint32]bool{}
	for _, j := range jogos {
		if j.UID != 0 {
			vistos[j.UID] = true
		}
	}
	if len(vistos) == 0 {
		if uid, ok := sessionUID(); ok {
			vistos[uid] = true
		}
	}
	for uid := range vistos {
		if err := notify.Show(uid, n.Urgency, n.Headline, n.Body); err != nil {
			logPrio(4, "horario: notificacao falhou: %v", err)
		}
	}
}

// sessionUID devolve o uid do unico usuario com sessao em /run/user (fora o
// root). Com mais de um, nao chutamos.
func sessionUID() (uint32, bool) {
	entries, err := os.ReadDir("/run/user")
	if err != nil {
		return 0, false
	}
	var uids []uint64
	for _, e := range entries {
		if uid, err := strconv.ParseUint(e.Name(), 10, 32); err == nil && uid != 0 {
			uids = append(uids, uid)
		}
	}
	if len(uids) != 1 {
		return 0, false
	}
	return uint32(uids[0]), true
}

func loadScheduleState() schedule.State {
	var st schedule.State
	if data, err := os.ReadFile(scheduleStatePath); err == nil {
		if err := json.Unmarshal(data, &st); err != nil {
			logPrio(4, "horario: estado corrompido, ignorando: %v", err)
			return schedule.State{}
		}
	}
	return st
}

func saveScheduleState(st schedule.State) {
	data, _ := json.Marshal(st)
	tmp := scheduleStatePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err == nil {
		err = os.Rename(tmp, scheduleStatePath)
		if err == nil {
			return
		}
	}
	logPrio(4, "horario: nao consegui gravar %s", scheduleStatePath)
}

func loadScheduleConfig(cmd *cobra.Command) (*schedule.Config, error) {
	path, _ := cmd.Flags().GetString("config")
	return schedule.Load(path)
}

// logNotice imprime uma mensagem. Sob o systemd (JOURNAL_STREAM definido),
// usa o prefixo "<5>" para gravar com prioridade notice, que passa pelo
// LogLevelMax=notice da unidade enquanto as mensagens de rotina sao filtradas.
func logNotice(format string, a ...any) { logPrio(5, format, a...) }

// logPrio imprime com a prioridade syslog dada quando o stdout e o journal;
// no terminal, imprime a mensagem sem prefixo.
func logPrio(prio int, format string, a ...any) {
	prefix := ""
	if stdoutIsJournal() {
		prefix = fmt.Sprintf("<%d>", prio)
	}
	fmt.Printf(prefix+format+"\n", a...)
}

// stdoutIsJournal segue a recomendacao do systemd: JOURNAL_STREAM ("dev:inode")
// pode ser herdado por processos filhos (ex: um terminal), entao so vale se
// bater com o dispositivo/inode do stdout atual.
func stdoutIsJournal() bool {
	var dev, ino uint64
	if _, err := fmt.Sscanf(os.Getenv("JOURNAL_STREAM"), "%d:%d", &dev, &ino); err != nil {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && uint64(st.Dev) == dev && st.Ino == ino
}

func estadoTexto(bloqueado bool) string {
	if bloqueado {
		return "BLOQUEADO"
	}
	return "liberado"
}

func weekdayPT(d time.Weekday) string {
	return [...]string{"domingo", "segunda", "terca", "quarta", "quinta", "sexta", "sabado"}[d]
}

func init() {
	scheduleCmd.PersistentFlags().String("config", schedule.DefaultConfigPath, "Arquivo de horario")
	scheduleStatusCmd.Flags().String("at", "", "Simular um instante: \"AAAA-MM-DD HH:MM\" (fuso do horario)")
	rootCmd.AddCommand(scheduleCmd)
	scheduleCmd.AddCommand(scheduleApplyCmd)
	scheduleCmd.AddCommand(scheduleStatusCmd)
}

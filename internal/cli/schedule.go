// Comandos "schedule": aplicam o horario semanal de /etc/open-turkey/schedule.json
// a um bloco. O estado e sempre recalculado a partir da hora atual, entao
// "schedule apply" pode rodar a qualquer momento (boot, volta da suspensao,
// timer de minuto em minuto) e e idempotente: so mexe no bloco se o estado
// atual diferir do horario.
package cli

import (
	"fmt"
	"os"
	"time"

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
		cfg, err := loadScheduleConfig(cmd)
		if err != nil {
			return err
		}
		return applySchedule(cfg, time.Now())
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
		if next, ok := cfg.NextChange(quando); ok {
			n := next.In(cfg.Location)
			fmt.Printf("Proxima mudanca: %s %s -> %s\n", weekdayPT(n.Weekday()), n.Format("2006-01-02 15:04"), estadoTexto(cfg.ShouldBlock(next)))
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

// applySchedule leva o bloco ao estado que o horario exige no instante agora.
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
	deveBloquear := cfg.ShouldBlock(agora)

	switch {
	case deveBloquear == detalhe.Active:
		// Ja esta certo. Nao imprimimos nada para nao poluir o journal
		// (este comando roda a cada minuto).
		return nil

	case deveBloquear:
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

	default:
		// Um bloco travado manualmente com --lock fica como esta: so o
		// desafio de "unlock" pode desativa-lo.
		if detalhe.Locked {
			logNotice("horario: bloco '%s' esta travado (--lock); nao desativado", cfg.Block)
			return nil
		}
		if err := database.DeactivateBlock(cfg.Block); err != nil {
			return err
		}
		if err := reaplicarOuRemoverCamadas(database); err != nil {
			return err
		}
		logNotice("horario: bloco '%s' DESATIVADO", cfg.Block)
	}
	return nil
}

func loadScheduleConfig(cmd *cobra.Command) (*schedule.Config, error) {
	path, _ := cmd.Flags().GetString("config")
	return schedule.Load(path)
}

// logNotice imprime uma mensagem. Sob o systemd (JOURNAL_STREAM definido),
// usa o prefixo "<5>" para gravar com prioridade notice, que passa pelo
// LogLevelMax=notice da unidade enquanto as mensagens de rotina sao filtradas.
func logNotice(format string, a ...any) {
	prefix := ""
	if os.Getenv("JOURNAL_STREAM") != "" {
		prefix = "<5>"
	}
	fmt.Printf(prefix+format+"\n", a...)
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

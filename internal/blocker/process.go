// =============================================================================
// Pacote blocker — Modulo de encerramento de processos do Open Turkey
// =============================================================================
//
// Este arquivo implementa o "matador de processos" — a parte do Open Turkey
// que fecha programas bloqueados (como navegadores ou apps de distracoes).
// Vamos entender como isso funciona no Linux:
//
// O QUE E UM PROCESSO?
// --------------------
// Todo programa rodando no seu computador e um "processo". Quando voce abre
// o Firefox, o Linux cria um processo pra ele. Cada processo recebe um numero
// unico chamado PID (Process ID), que e como um RG — nenhum outro processo
// ativo tem o mesmo numero.
//
// O QUE E O /proc?
// -----------------
// O Linux tem um sistema de arquivos virtual chamado /proc. Ele nao existe de
// verdade no disco — o kernel (nucleo do sistema) gera essas informacoes em
// tempo real. Cada processo ativo tem uma pasta /proc/[PID]/ com varias
// informacoes sobre ele. E como se cada processo tivesse uma "ficha cadastral"
// que o kernel mantem atualizada.
//
// Dois arquivos nos interessam:
//
//	/proc/[PID]/comm    → O nome curto do processo (maximo 15 caracteres).
//	                      Exemplo: "firefox", "chrome", "slack"
//
//	/proc/[PID]/cmdline → A linha de comando completa usada pra iniciar o
//	                      processo. Exemplo: "/usr/bin/firefox --new-window".
//	                      Os argumentos sao separados por bytes nulos (\0),
//	                      nao por espacos — isso e uma particularidade do Linux.
//
// COMO "MATAMOS" UM PROCESSO?
// ----------------------------
// No Linux, processos se comunicam atraves de "sinais" (signals). Sao como
// mensagens especiais que o kernel entrega. Existem varios tipos:
//
//	SIGTERM (sinal 15) → "Por favor, encerre educadamente." O processo pode
//	                      capturar esse sinal, salvar dados e sair com calma.
//
//	SIGKILL (sinal 9)  → "Morra AGORA." O processo NAO pode capturar, ignorar
//	                      ou bloquear esse sinal. O kernel mata ele na hora,
//	                      sem chance de fazer nada. E o "botao de emergencia".
//
// Nos usamos SIGKILL porque estamos lidando com programas que o usuario quer
// BLOQUEADOS. Nao faz sentido pedir educadamente pro Firefox fechar — o
// usuario pode ter uma aba aberta no site bloqueado e o navegador poderia
// simplesmente ignorar o pedido.
//
// POR QUE PRECISAMOS DE ROOT?
// ---------------------------
// Um usuario normal so pode matar processos que pertencem a ele. Mas como o
// Open Turkey roda como root (precisa disso pra editar /etc/hosts e o
// iptables), ele tambem tem permissao pra matar qualquer processo no sistema.
//
// CUIDADOS DE SEGURANCA:
// ----------------------
//   - Nunca matamos o PID 1 (init/systemd) — ele e o "pai" de todos os
//     processos. Mata-lo derrubaria o sistema inteiro.
//   - Nunca matamos nosso proprio processo — seria suicidio, o bloqueio pararia.
//   - Processos podem desaparecer a qualquer momento (o usuario fechou, ou o
//     processo terminou sozinho). Isso causa "race conditions" — tentamos ler
//     a ficha de um processo que ja nao existe mais. Simplesmente ignoramos
//     esses erros e seguimos em frente.
//
// =============================================================================
package blocker

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// KillBlocked percorre todos os processos ativos no sistema e encerra (mata)
// aqueles cujos nomes batem com a lista fornecida.
//
// Parametros:
//   - processNames: lista de nomes de programas a serem mortos.
//     Exemplos: ["firefox", "chrome", "slack", "discord"]
//
// Retorno:
//   - int:   quantidade de processos que foram efetivamente mortos.
//   - error: erro caso nao consiga ler o diretorio /proc (se isso falhar,
//     algo esta muito errado no sistema).
//
// Exemplo de uso:
//
//	killed, err := blocker.KillBlocked([]string{"firefox", "chrome"})
//	fmt.Printf("Processos encerrados: %d\n", killed)
func KillBlocked(processNames []string) (int, error) {
	// Se a lista de processos esta vazia, nao temos nada pra fazer.
	// Retornamos 0 mortos e nenhum erro.
	// Defesa extra: descartamos nomes invalidos ou criticos mesmo que ja
	// estejam no banco (gravados antes de existir a validacao no add-app).
	processNames = filtrarNomesSeguros(processNames)
	if len(processNames) == 0 {
		return 0, nil
	}

	// Lemos o conteudo do diretorio /proc. Cada entrada pode ser:
	// - Um numero (ex: "1234") → e uma pasta de processo (PID)
	// - Um nome (ex: "cpuinfo", "meminfo") → e um arquivo do sistema, ignoramos
	entries, err := os.ReadDir("/proc")
	if err != nil {
		// Se nao conseguimos ler /proc, algo esta muito errado.
		// Pode ser que o sistema nao tenha /proc montado (muito raro no Linux).
		return 0, err
	}

	// Pegamos nosso proprio PID pra nao nos matarmos acidentalmente.
	// os.Getpid() retorna o PID do programa que esta rodando AGORA (o Open Turkey).
	selfPID := os.Getpid()

	// Contador de quantos processos matamos com sucesso.
	killed := 0

	// Percorremos cada entrada do /proc procurando por PIDs.
	for _, entry := range entries {
		// Tentamos converter o nome da entrada pra numero.
		// Se funcionar, e um PID. Se nao, e um arquivo do sistema e ignoramos.
		// Exemplo: "1234" vira o numero 1234, mas "cpuinfo" daria erro.
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			// Nao e um numero → nao e um PID → pula pro proximo.
			continue
		}

		// SEGURANCA: Nunca matamos o PID 1 (init/systemd).
		// O PID 1 e o primeiro processo que o kernel inicia. Ele e o "avo"
		// de todos os outros processos. Mata-lo causaria um kernel panic
		// ou desligaria o sistema.
		if pid == 1 {
			continue
		}

		// SEGURANCA: Nunca matamos nosso proprio processo.
		// Se nos matassemos, o Open Turkey pararia de funcionar e todos os
		// bloqueios que dependem de monitoramento continuo falhariam.
		if pid == selfPID {
			continue
		}

		// Verificamos se este processo bate com algum nome da lista de bloqueio.
		// Usamos duas estrategias de deteccao (explicadas abaixo).
		if matchesBlockedProcess(pid, processNames) {
			// Encontramos um processo bloqueado! Enviamos SIGKILL (sinal 9).
			//
			// syscall.Kill() pede ao kernel pra enviar um sinal a um processo.
			// SIGKILL e o sinal mais agressivo — mata o processo instantaneamente
			// e ele NAO pode interceptar ou ignorar esse sinal.
			err := syscall.Kill(pid, syscall.SIGKILL)
			if err != nil {
				// Se deu erro ao matar, o processo provavelmente ja morreu sozinho
				// entre a hora que lemos /proc e a hora que tentamos mata-lo.
				// Isso e uma "race condition" — situacao normal em sistemas com
				// muitos processos. Simplesmente ignoramos e continuamos.
				continue
			}
			// Processo morto com sucesso! Incrementamos o contador.
			killed++
		}
	}

	// Retornamos quantos processos matamos. Nao retornamos erro aqui porque
	// erros individuais de processos que desapareceram sao normais e ja foram
	// tratados acima.
	return killed, nil
}

// matchesBlockedProcess le as informacoes de identidade de um processo em
// /proc/[pid]/ e decide, via processMatches, se ele esta na lista de bloqueio.
//
// Usamos tres fontes, sempre com comparacao EXATA (nunca substring):
//
//  1. /proc/[pid]/comm — nome curto do processo (ate 15 caracteres).
//  2. /proc/[pid]/exe  — link simbolico para o executavel real; usamos so
//     o basename (ex: "/opt/steam/steamwebhelper" → "steamwebhelper").
//  3. argv[0] de /proc/[pid]/cmdline — so o basename do PRIMEIRO argumento.
//
// Por que nao olhamos os outros argumentos? Porque o daemon roda como root e
// mata com SIGKILL: comparar por substring em todo o cmdline faria
// "vim notas-steam.txt" ser morto ao bloquear "steam".
func matchesBlockedProcess(pid int, processNames []string) bool {
	pidPath := "/proc/" + strconv.Itoa(pid)

	// Erros de leitura sao normais (processo sumiu, ou /proc/[pid]/exe de
	// threads do kernel) — o campo correspondente so fica vazio.
	var comm, exe, argv0 string
	if b, err := os.ReadFile(pidPath + "/comm"); err == nil {
		comm = strings.TrimSpace(string(b))
	}
	if target, err := os.Readlink(pidPath + "/exe"); err == nil {
		exe = target
	}
	if b, err := os.ReadFile(pidPath + "/cmdline"); err == nil {
		argv0 = argv0FromCmdline(b)
	}

	return processMatches(comm, exe, argv0, processNames)
}

// processMatches e a regra de decisao pura (sem acesso ao /proc), para poder
// ser testada. Um processo bate se algum nome da lista for EXATAMENTE igual a:
// comm, basename(exe) ou basename(argv0). Nomes vazios nunca batem.
func processMatches(comm, exe, argv0 string, processNames []string) bool {
	// Quando o executavel e substituido em disco (ex: o Steam se atualizando),
	// o kernel mostra o link como "/caminho/steam (deleted)".
	exe = strings.TrimSuffix(exe, " (deleted)")

	candidates := []string{comm, baseName(exe), baseName(argv0)}
	for _, name := range processNames {
		if name == "" {
			continue
		}
		for _, c := range candidates {
			if c == name {
				return true
			}
		}
	}
	return false
}

// argv0FromCmdline extrai so o primeiro argumento do conteudo de
// /proc/[pid]/cmdline, cujos argumentos sao separados por bytes nulos (\0).
func argv0FromCmdline(cmdline []byte) string {
	argv0, _, _ := strings.Cut(string(cmdline), "\x00")
	return argv0
}

// baseName devolve o ultimo componente de um caminho ("/usr/bin/steam" →
// "steam"). Diferente de filepath.Base, devolve "" para entrada vazia em vez
// de ".", para que um campo ausente nunca bata com nada.
func baseName(path string) string {
	if path == "" {
		return ""
	}
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// filtrarNomesSeguros devolve so os nomes aceitos por ValidarNomeProcesso.
func filtrarNomesSeguros(processNames []string) []string {
	var seguros []string
	for _, n := range processNames {
		if ValidarNomeProcesso(n) == nil {
			seguros = append(seguros, n)
		}
	}
	return seguros
}

// ProcessInfo identifica um processo encontrado por FindProcesses.
type ProcessInfo struct {
	PID int
	UID uint32
}

// FindProcesses lista os processos cujo nome bate EXATAMENTE (mesma regra de
// KillBlocked) com algum de processNames, sem mata-los. O UID e o dono do
// processo (dono do diretorio /proc/[pid]).
func FindProcesses(processNames []string) ([]ProcessInfo, error) {
	if len(processNames) == 0 {
		return nil, nil
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var found []ProcessInfo
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !matchesBlockedProcess(pid, processNames) {
			continue
		}
		fi, err := os.Stat("/proc/" + entry.Name())
		if err != nil {
			continue // o processo sumiu
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			found = append(found, ProcessInfo{PID: pid, UID: st.Uid})
		}
	}
	return found, nil
}

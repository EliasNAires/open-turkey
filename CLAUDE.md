# Open Turkey

Bloqueador de produtividade para Linux via linha de comando (CLI) + serviço `systemd`.

Organiza o bloqueio em **blocos** (grupos nomeados) de **sites** (domínios) e
**apps** (nomes de processos). Ao ativar um bloco, um daemon systemd garante
continuamente que as camadas de bloqueio estejam aplicadas.

## Build

```bash
make build            # como usuário: CGO_ENABLED=0 go build -o open-turkey ./cmd/open-turkey
sudo ./install.sh     # NÃO compila (Go pode estar só no PATH do usuário, ex: mise);
                      # instala binário, wrappers, sudoers, systemd, horário e DB
```

Build sem CGo porque o driver SQLite é `modernc.org/sqlite` (Go puro). Para
verificar a compilação inteira: `CGO_ENABLED=0 go build ./...`.

## Arquitetura

- **Entrypoint** (`cmd/open-turkey/main.go`): enxuto, só delega a `cli.Execute()`.
- **CLI** (`internal/cli/`): comandos Cobra. `block.go` (CRUD de blocos),
  `control.go` (`start`/`stop`/`unlock`/`status`), `schedule.go`
  (`schedule apply|status`).
- **Horário** (`internal/schedule/`): janelas semanais de desbloqueio lidas de
  `/etc/open-turkey/schedule.json`; `ShouldBlock(t)` é pura (hora injetada).
  `open-turkey-schedule.timer` roda `schedule apply` a cada minuto e no boot;
  `open-turkey-schedule-resume.service` ao voltar da suspensão.
- **Camadas de bloqueio** (`internal/blocker/`): `/etc/hosts`, firewall iptables,
  políticas de navegador (Firefox/Chrome/Chromium) e kill de processos.
  Processos batem só por nome EXATO (comm, basename de exe ou de argv[0]);
  `ValidarNomeProcesso` rejeita nomes curtos e críticos.
- **Daemon** (`internal/daemon/`): serviço systemd, reaplica as camadas a cada 5s
  caso algo seja alterado manualmente.
- **DB** (`internal/db/`): SQLite em `/var/lib/open-turkey/open-turkey.db` (WAL).
- **Lock** (`internal/lock/`): desafio de digitação que trava a desativação de um
  bloco (`--lock`), criando atrito contra desativação impulsiva.

## Instalação no sistema

`make install` copia tudo para fora do diretório-fonte: binário real em
`/usr/local/bin/open-turkey-bin`, wrappers `open-turkey` e `open-turkey-schedule`
(fazem `exec sudo`), regra em `/etc/sudoers.d/open-turkey` só para o usuário que
instalou, unidade `open-turkey.service` e o DB em
`/var/lib/open-turkey/`. O programa instalado não depende da pasta do projeto.

## Comandos

Uso e exemplos completos no `README.md`. Resumo:
`block create|add-site|add-app|remove-site|remove-app|list|info|remove`,
`start [--lock]`, `stop`, `unlock`, `status`.

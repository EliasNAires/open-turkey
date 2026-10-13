# =============================================================================
# Open Turkey - Makefile
# Bloqueador de produtividade para Linux
# =============================================================================

# Nome do binario final (compilado pelo Go)
BINARY_NAME=open-turkey

# Nome do binario instalado no sistema
# Renomeamos para "open-turkey-bin" para separar o binario real
# do wrapper script que o usuario executa diretamente
BINARY_INSTALLED=open-turkey-bin

# Caminho de instalacao do binario
INSTALL_PATH=/usr/local/bin

# Caminho do arquivo sudoers que permite executar sem senha
# Usamos /etc/sudoers.d/ (diretorio de "drop-in") em vez de editar
# /etc/sudoers diretamente — assim a regra e isolada e facil de remover
SUDOERS_FILE=/etc/sudoers.d/open-turkey

# Caminho para instalar o arquivo de servico do systemd
SERVICE_PATH=/etc/systemd/system

# Diretorio para armazenar o banco de dados SQLite
DB_DIR=/var/lib/open-turkey

# Inclui o Go instalado em /usr/local/go/bin no PATH
export PATH := /usr/local/go/bin:$(PATH)

# -------------------------------------------------------
# build: compila o binario
# Usa CGO_ENABLED=0 pois utilizamos modernc.org/sqlite
# (implementacao pura em Go, sem dependencia de CGo)
# -------------------------------------------------------
.PHONY: build
build:
	@echo ">> Compilando o $(BINARY_NAME)..."
	CGO_ENABLED=0 go build -o $(BINARY_NAME) ./cmd/open-turkey

# -------------------------------------------------------
# install: compila, copia o binario, instala o servico
#          do systemd, cria o diretorio do banco,
#          configura sudoers e cria o wrapper script
#
# Apos a instalacao, o usuario pode executar:
#   open-turkey start ...
# sem precisar digitar "sudo" nem senha.
# -------------------------------------------------------
#
# O install NAO compila: o Go pode estar instalado so para o usuario
# (ex: via mise), fora do PATH do root. Rode "make build" como usuario
# antes de "sudo ./install.sh".
# -------------------------------------------------------
.PHONY: install
install:
	@test -x $(BINARY_NAME) || { echo "Erro: binario '$(BINARY_NAME)' nao encontrado. Rode 'make build' (sem sudo) antes."; exit 1; }
	@echo ">> Instalando o binario real como $(BINARY_INSTALLED)..."
	# O binario real fica com outro nome para que o wrapper
	# script possa ocupar o nome "open-turkey"
	# Copiamos para um arquivo temporário e fazemos um rename atômico.
	# Isso evita erro "Área de texto ocupada" (ETXTBSY) quando o daemon
	# está executando o binário atual.
	tmp="$(INSTALL_PATH)/.$(BINARY_INSTALLED).tmp.$$"; \
	cp $(BINARY_NAME) "$$tmp"; \
	chmod 755 "$$tmp"; \
	mv -f "$$tmp" $(INSTALL_PATH)/$(BINARY_INSTALLED)

	@echo ">> Criando wrapper script em $(INSTALL_PATH)/$(BINARY_NAME)..."
	# O wrapper script e o que o usuario executa ao digitar "open-turkey".
	# Ele chama "sudo open-turkey-bin" por baixo dos panos.
	# "exec" substitui o processo do shell pelo sudo, evitando um
	# processo intermediario desnecessario na arvore de processos.
	# "$$@" repassa todos os argumentos do usuario (ex: start, block, etc.)
	printf '#!/bin/bash\n\
# Wrapper script do Open Turkey\n\
# Chama o binario real (open-turkey-bin) com sudo, de forma transparente.\n\
# A regra em /etc/sudoers.d/open-turkey permite isso sem pedir senha.\n\
# "exec" substitui este processo pelo sudo — nao fica shell sobrando.\n\
exec sudo $(INSTALL_PATH)/$(BINARY_INSTALLED) "$$@"\n' > $(INSTALL_PATH)/$(BINARY_NAME)
	chmod 755 $(INSTALL_PATH)/$(BINARY_NAME)

	@echo ">> Configurando sudoers para execucao sem senha..."
	# Regra sudoers: permite que SOMENTE o usuario que rodou o instalador
	# ($SUDO_USER) execute open-turkey-bin como root, sem senha.
	#   <usuario> ALL=(root) NOPASSWD: /usr/local/bin/open-turkey-bin
	#
	# O arquivo e gerado num temporario, validado com "visudo -cf" e so
	# entao movido para o lugar — um sudoers invalido quebraria o sudo.
	# chmod 440: obrigatorio, o sudo recusa arquivos com permissoes abertas.
	@case "$$SUDO_USER" in \
	  ""|root) echo "Erro: rode via 'sudo' a partir do seu usuario (SUDO_USER vazio ou root)."; exit 1;; \
	esac; \
	echo "$$SUDO_USER" | grep -Eq '^[a-z_][a-z0-9_-]*$$' || { echo "Erro: nome de usuario invalido: $$SUDO_USER"; exit 1; }; \
	id -u "$$SUDO_USER" >/dev/null || exit 1; \
	tmp=$$(mktemp); \
	printf '%s ALL=(root) NOPASSWD: $(INSTALL_PATH)/$(BINARY_INSTALLED)\n' "$$SUDO_USER" > "$$tmp"; \
	chmod 440 "$$tmp"; \
	visudo -cf "$$tmp" || { rm -f "$$tmp"; echo "Erro: regra sudoers invalida"; exit 1; }; \
	install -o root -g root -m 440 "$$tmp" $(SUDOERS_FILE); \
	rm -f "$$tmp"

	@echo ">> Instalando o servico do systemd..."
	cp systemd/open-turkey.service $(SERVICE_PATH)/open-turkey.service
	chmod 644 $(SERVICE_PATH)/open-turkey.service

	@echo ">> Criando diretorio do banco de dados em $(DB_DIR)..."
	mkdir -p $(DB_DIR)

	@echo ">> Habilitando e iniciando o servico..."
	systemctl daemon-reload
	systemctl enable open-turkey.service
	systemctl start open-turkey.service

	@echo ">> Instalacao concluida com sucesso!"

# -------------------------------------------------------
# uninstall: para o servico, desabilita, remove o binario
#            e o arquivo de servico do systemd
# -------------------------------------------------------
.PHONY: uninstall
uninstall:
	@echo ">> Parando o servico..."
	-systemctl stop open-turkey.service

	@echo ">> Desabilitando o servico..."
	-systemctl disable open-turkey.service

	@echo ">> Removendo o arquivo de servico..."
	rm -f $(SERVICE_PATH)/open-turkey.service
	systemctl daemon-reload

	@echo ">> Removendo o wrapper script e o binario real..."
	rm -f $(INSTALL_PATH)/$(BINARY_NAME)
	rm -f $(INSTALL_PATH)/$(BINARY_INSTALLED)

	@echo ">> Removendo regra sudoers..."
	rm -f $(SUDOERS_FILE)

	@echo ">> Desinstalacao concluida com sucesso!"

# -------------------------------------------------------
# clean: remove artefatos de compilacao
# -------------------------------------------------------
.PHONY: clean
clean:
	@echo ">> Limpando artefatos de compilacao..."
	rm -f $(BINARY_NAME)
	@echo ">> Limpeza concluida!"

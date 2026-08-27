PROJECT=$(shell basename $(CURDIR))
VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo v0.0.0-dev)
LDFLAGS := -X paepcke.de/$(PROJECT)/internal/version.Version=$(VERSION)
TARGETBIN := /nix/persist/bin

all: build

build:
	touch $(PROJECT) && rm $(PROJECT)
	go build -ldflags "$(LDFLAGS)" -o ./${PROJECT} ./cmd/$(PROJECT)

update: 
	git pull
	git pull --force --tags 

deploy-test-nix: update build 
	sudo -v
	sudo mkdir -p $(TARGETBIN)
	sudo mv -f ./$(PROJECT) $(TARGETBIN)

run: build 
	OLLAMA_DESC_URL="http://aiworker02.dbt.corp:11434" \
	OLLAMA_DESC_MODEL="gpt-oss:latest" \
	OMC_SIGN_KEY_PATH="~/.ssh/agent" \
	OMC_NAME="PAECPKE, Michael" \
	OMC_EMAIL="git@paepcke.de" \
	./$(PROJECT)

deps:
	rm -rf go.mod go.sum
	go mod init paepcke.de/$(PROJECT)
	go mod tidy -v
	git config core.fileMode false

check:
	go fmt ./...
	go vet ./...
	go mod tidy -diff

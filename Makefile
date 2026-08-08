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
	OLLAMA_DESC_URL="http://aiworker02.dbt.corp:11434/v1" \
	OLLAMA_DESC_MODEL="gpt-oss:latest" \
	OCOMMIT_KEY_PATH="..." \
	OCOMMIT_NAME="PAECPKE, Michael" \
	OCOMMIT_EMAIL="git@paepcke.de" \
	./$(PROJECT)

deps:
	git config core.fileMode false
	rm -rf go.mod go.sum
	go mod init paepcke.de/$(PROJECT)
	go mod tidy -v

check:
	gofmt -l .
	go vet ./...
	go mod tidy -diff

test:
	go test -race -count=1 ./...

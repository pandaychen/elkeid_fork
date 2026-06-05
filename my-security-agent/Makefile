PRODUCT  := my-security-agent
VERSION  := 1.0.0
LDFLAGS  := -X main.Version=$(VERSION) -s -w

.PHONY: all agent ctl plugin clean install

all: agent ctl plugin

agent:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(PRODUCT) ./cmd/agent/

ctl:
	CGO_ENABLED=0 go build -ldflags "-s -w" -o $(PRODUCT)ctl ./cmd/agentctl/

plugin:
	CGO_ENABLED=0 go build -ldflags "-s -w" -o process-collector ./plugins/process-collector/

clean:
	rm -f $(PRODUCT) $(PRODUCT)ctl process-collector

install: all
	sudo bash deploy/install.sh

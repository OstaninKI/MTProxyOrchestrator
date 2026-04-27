BINDIR := bin

.PHONY: test build lint check

test:
	go test ./...

build:
	mkdir -p $(BINDIR)
	go build -o $(BINDIR)/tgproxy-cli ./cmd/tgproxy-cli
	go build -o $(BINDIR)/tgproxy-panel ./cmd/tgproxy-panel

lint:
	go vet ./...

check:
	@gofmt -l . | grep -q . && echo "gofmt: formatting issues found (run: gofmt -w .)" && exit 1 || true
	$(MAKE) lint
	$(MAKE) test

.PHONY: build proto release clean audit sha tools

FLAGS=-trimpath -buildvcs=false -tags='netgo,osusergo,static_build'
LDFLAGS=-ldflags='-s -w -extldflags "-static"'

all: build

build:
	@go mod tidy
	CGO_ENABLED=0 go build ${FLAGS} ${LDFLAGS} -o ./bin/ ./cmd/swap

build-api:
	CGO_ENABLED=0 go build ${FLAGS} ${LDFLAGS} -o ./bin/httpapi ./cmd/httpapi

run-api:
	SWAP_QUOTE_PROVIDER=0x \
	SWAP_0X_API_KEY=$(SWAP_0X_API_KEY) \
	SWAP_0X_CHAIN_ID=$(or $(SWAP_0X_CHAIN_ID),1) \
	SWAP_0X_TAKER=$(or $(SWAP_0X_TAKER),0x0000000000000000000000000000000000010000) \
	go run ./cmd/httpapi

test-api:
	go test ./cmd/httpapi/ -v -count=1

pre: audit
	go mod tidy
	go fmt ./... && go vet ./...
	
release: proto release-client
release-client:
	goreleaser release --clean --skip=announce,validate
release-dev:
	GORELEASER_CURRENT_TAG="v0.0.1" goreleaser release --clean --skip=announce,validate --snapshot --skip-publish

docker: build docker-build docker-push
	docker build -t lfaoro/swap:latest .
	docker push lfaoro/swap:latest
docker-build:
	docker build -t lfaoro/swap:latest .
docker-push:
	docker push lfaoro/swap:latest

buildnix:
	nix flake init
	nix build

proto:
	rm -rf proto/go
	buf generate
	
clean:
	rm -rf gen/* bin/*

audit:
	gosec ./app ./cmd/swap

sha:
	shasum -a256 ./bin/swap | tee ./bin/swap.sum

update:
	go get -u ./cmd/swap

loc:
	find . -name "*.go" -not -path "*/src/*" -not -path "*/gen/*" -not -path "*/vendor/*" -not -path "*/test/*" | xargs wc -l

upgrade:
	go get -u ./cmd/swap

tools:
	go install github.com/air-verse/air@latest
	go install github.com/bufbuild/buf/cmd/buf@latest
	go install github.com/securego/gosec/v2/cmd/gosec@latest

backup: clean
	tar -czvf ../swapcli-$(shell date +%Y%m%d).tgz --exclude='.git' .

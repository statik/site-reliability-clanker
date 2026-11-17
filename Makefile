.PHONY: build web demo test lint destroy clean-data bundle release

build:
	go build -o bin/clanker ./cmd/clanker

web:
	cd web && pnpm install --frozen-lockfile && pnpm build

# One command from clone to the running demo. Open http://127.0.0.1:7070 when it listens.
demo: build web
	./bin/clanker up

# The files that a released binary carries, so that it needs no checkout. Only committed
# files go in, and the built dashboard.
bundle: web
	git archive --format=tar HEAD platform runbooks apps/paperboat > internal/bundle/bundle.tar
	tar -rf internal/bundle/bundle.tar web/dist
	gzip -9 -f internal/bundle/bundle.tar

VERSION ?= $(shell git describe --tags --always --dirty)
PLATFORMS := darwin-arm64 darwin-amd64 linux-arm64 linux-amd64

# Self-contained binaries for each platform, with checksums, in dist/.
release: bundle
	mkdir -p dist
	for platform in $(PLATFORMS); do \
		CGO_ENABLED=0 GOOS=$${platform%-*} GOARCH=$${platform#*-} go build -tags bundle -trimpath \
			-ldflags "-s -w -X main.version=$(VERSION)" -o dist/clanker-$$platform ./cmd/clanker || exit 1; \
	done
	cd dist && shasum -a 256 clanker-* > checksums.txt

test:
	go test ./...
	cd apps/paperboat && go test ./...
	opa test runbooks/policy

lint:
	test -z "$$(gofmt -l cmd internal apps/paperboat)"
	go vet ./...
	cd apps/paperboat && go vet ./...
	tofu fmt -check -recursive platform
	opa check runbooks/policy
	shellcheck runbooks/jobs/*.sh apps/paperboat/build.sh
	shfmt -i 2 -d runbooks/jobs apps/paperboat/build.sh

destroy: build
	./bin/clanker destroy --env local

# Removes the control-plane database, repositories and logs. Destroy the environment first.
clean-data:
	for name in $$(docker ps -a --filter label=clanker.data=$(abspath .clanker) --format '{{.Names}}'); do \
		docker rm -f $$name && docker volume rm $$name; \
	done
	trash .clanker

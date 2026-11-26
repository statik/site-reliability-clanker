.PHONY: build web demo test lint lint-core destroy clean-data bundle release dmg runner-image

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

# The macOS app in a disk image, dist/SRC.dmg. docs/MACOS-RELEASE.md has the settings
# for a signed and notarized image. Without them the image is for a local test only.
dmg: release
	scripts/macos/package.sh $(VERSION)
	cd dist && shasum -a 256 clanker-* SRC.dmg > checksums.txt

RUNNER_IMAGE ?= clanker-runner:$(VERSION)

# The image of the runner for a Fargate task on ARM64, loaded into the local Docker. The
# binary in it has the flags of a release, from the same commit as the core.
runner-image: bundle
	docker buildx build --platform linux/arm64 --build-arg VERSION=$(VERSION) \
		--file build/runner/Dockerfile --tag $(RUNNER_IMAGE) --load .

test:
	go test ./...
	cd apps/paperboat && go test ./...
	opa test runbooks/policy

# The core of the control plane must run as a unikernel, which cannot start a program.
CORE_PACKAGES := agent api applayer audit awsrunner awstarget chatops detect dispatch forge jobspec monitor notify observation ollama orchestrator otlp pack planfacts policy runbook scrub signin store

lint-core:
	scripts/lint-core.sh $(CORE_PACKAGES)

# The packs in apps/ and packs/, and the runbook scripts of the packs in packs/.
PACK_DIRS := $(wildcard apps/*/ packs/*/)
PACK_SCRIPTS := $(wildcard packs/*/runbooks/*.sh)

lint: lint-core
	test -z "$$(gofmt -l cmd internal scripts apps/paperboat)"
	for dir in $(PACK_DIRS); do go run ./cmd/clanker pack validate $$dir || exit 1; done
	go vet ./...
	cd apps/paperboat && go vet ./...
	tofu fmt -check -recursive platform internal/setup/stack
	opa check runbooks/policy
	shellcheck apps/paperboat/runbooks/*.sh apps/paperboat/build.sh ops/run-local.sh scripts/*.sh \
		scripts/verify/*.sh scripts/macos/*.sh build/runner/*.sh internal/setup/stack/*/*.sh \
		platform/modules/*/*/*.sh $(PACK_SCRIPTS)
	shfmt -i 2 -d apps/paperboat/runbooks apps/paperboat/build.sh ops/run-local.sh scripts \
		build/runner internal/setup/stack platform/modules $(PACK_SCRIPTS)

destroy: build
	./bin/clanker destroy --env local

# Removes the control-plane database, repositories and logs. Destroy the environment first.
clean-data:
	for name in $$(docker ps -a --filter label=clanker.data=$(abspath .clanker) --format '{{.Names}}'); do \
		docker rm -f $$name && docker volume rm $$name; \
	done
	trash .clanker

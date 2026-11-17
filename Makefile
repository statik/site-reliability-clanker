.PHONY: build web demo test lint destroy clean-data

build:
	go build -o bin/clanker ./cmd/clanker

web:
	cd web && pnpm install --frozen-lockfile && pnpm build

# One command from clone to the running demo. Open http://127.0.0.1:7070 when it listens.
demo: build web
	./bin/clanker up

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
	shellcheck runbooks/jobs/*.sh apps/paperboat/build.sh site/*.sh
	shfmt -i 2 -d runbooks/jobs apps/paperboat/build.sh

destroy: build
	./bin/clanker destroy --env local

# Removes the control-plane database, repositories and logs. Destroy the environment first.
clean-data:
	trash .clanker

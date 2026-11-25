# The runner image: `clanker runner` and the tools that execute one job. On AWS each job
# runs in a Fargate task (ARM64) from this image. Build it with `make runner-image`, which
# makes the bundle first: the runner protocol has no version, so the runner must come from
# the same commit as the core.
#
# Each tool is pinned to a version and to the SHA-256 of its download for each
# architecture. The hashes come from the signed checksum files of OpenTofu and Helm, from
# dl.k8s.io for kubectl, and from the AWS CLI installers, whose PGP signatures were
# checked when the version was chosen.

FROM --platform=$BUILDPLATFORM golang:1.27.3-trixie@sha256:af1bfc2614ebb1dbd31af514b78740fd678850e76498762b425ecee4c5c6ea80 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
# The flags of `make release`. The tag `bundle` embeds internal/bundle/bundle.tar.gz.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    if [ ! -f internal/bundle/bundle.tar.gz ]; then \
      echo "internal/bundle/bundle.tar.gz is missing: build with make runner-image" >&2; \
      exit 1; \
    fi; \
    CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" go build -tags bundle -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" -o /out/clanker ./cmd/clanker

FROM debian:trixie-slim@sha256:8aa5a7bf5a1955bddbe7d74fcdd0b91ab30e74362a8b73af672b0481b35b44a0 AS tools
ARG TARGETARCH
ARG TOFU_VERSION=1.12.3
ARG TOFU_SHA256_ARM64=98861dad4ac08882dcf255fe5d48bed7deddc34e8d6544f8690c65212320ad64
ARG TOFU_SHA256_AMD64=f87d45408dcbb963ddbe10b2f75e55b3495310556e05c35765dfd5913551f182
ARG KUBECTL_VERSION=1.37.2
ARG KUBECTL_SHA256_ARM64=fcc16f63186659d9722014b7e02d5fdd2d4e669cb8c0ab66365d7d3b8ba03509
ARG KUBECTL_SHA256_AMD64=c04487523e7aa7d1493f7808174c899db93d7a9cf57531420cd76b9bd415f8f4
ARG HELM_VERSION=4.3.2
ARG HELM_SHA256_ARM64=df31cb61680ef728f82c770e5280dd565617bee6b039d37e85c677288be4bbb0
ARG HELM_SHA256_AMD64=d1c0278d46beab79582d7e50f5b3dc206944ba8e577cde618513fd07daa53983
ARG AWSCLI_VERSION=2.42.2
ARG AWSCLI_SHA256_ARM64=efda2e5d9d01726f47c491a7b006f55611e682a0bdb08f4d65594e27bdc6ab30
ARG AWSCLI_SHA256_AMD64=eb2181c8f391ee63acbadf53b13a80efe8702cbfef6a7da99a57e60631b47665
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
# This stage is thrown away, so the versions of curl and unzip do not reach the image.
# hadolint ignore=DL3008
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl unzip \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /download
RUN case "$TARGETARCH" in \
      arm64) aws_arch=aarch64; tofu_sum="$TOFU_SHA256_ARM64"; \
             kubectl_sum="$KUBECTL_SHA256_ARM64"; helm_sum="$HELM_SHA256_ARM64"; \
             aws_sum="$AWSCLI_SHA256_ARM64" ;; \
      amd64) aws_arch=x86_64; tofu_sum="$TOFU_SHA256_AMD64"; \
             kubectl_sum="$KUBECTL_SHA256_AMD64"; helm_sum="$HELM_SHA256_AMD64"; \
             aws_sum="$AWSCLI_SHA256_AMD64" ;; \
      *) echo "no tools are pinned for the architecture $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    tofu_release="https://github.com/opentofu/opentofu/releases/download/v${TOFU_VERSION}"; \
    curl -fsSLo tofu.tar.gz "${tofu_release}/tofu_${TOFU_VERSION}_linux_${TARGETARCH}.tar.gz"; \
    curl -fsSLo kubectl \
      "https://dl.k8s.io/release/v${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl"; \
    curl -fsSLo helm.tar.gz \
      "https://get.helm.sh/helm-v${HELM_VERSION}-linux-${TARGETARCH}.tar.gz"; \
    curl -fsSLo awscli.zip \
      "https://awscli.amazonaws.com/awscli-exe-linux-${aws_arch}-${AWSCLI_VERSION}.zip"; \
    printf '%s  %s\n' "$tofu_sum" tofu.tar.gz "$kubectl_sum" kubectl "$helm_sum" helm.tar.gz \
      "$aws_sum" awscli.zip | sha256sum --check --strict; \
    mkdir -p /out/bin; \
    tar -xzf tofu.tar.gz --no-same-owner -C /out/bin tofu; \
    tar -xzf helm.tar.gz --no-same-owner --strip-components=1 -C /out/bin \
      "linux-${TARGETARCH}/helm"; \
    install -m 0755 kubectl /out/bin/kubectl; \
    unzip -q awscli.zip; \
    ./aws/install --install-dir /opt/aws-cli --bin-dir /out/bin

FROM debian:trixie-slim@sha256:8aa5a7bf5a1955bddbe7d74fcdd0b91ab30e74362a8b73af672b0481b35b44a0
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
# Debian removes a package version from its mirrors when a security update replaces it, so
# a pinned version would break the build. The digest of the base image fixes the release.
# hadolint ignore=DL3008
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git \
    && rm -rf /var/lib/apt/lists/*
RUN groupadd --gid 10001 runner \
    && useradd --uid 10001 --gid runner --create-home --shell /usr/sbin/nologin runner \
    && install -d -o runner -g runner /var/lib/clanker-runner
COPY --from=tools /opt/aws-cli /opt/aws-cli
COPY --from=tools /out/bin/ /usr/local/bin/
USER 10001:10001
# The cache that `runner.New` uses below --data. The build fails when a lock file lacks the
# hash of a provider for this platform, because a job would then download it.
RUN --mount=type=bind,source=platform,target=/tmp/platform \
    --mount=type=bind,source=build/runner/cache-providers.sh,target=/tmp/cache-providers.sh \
    bash /tmp/cache-providers.sh /tmp/platform /var/lib/clanker-runner/plugin-cache
COPY --from=build /out/clanker /usr/local/bin/clanker
ENTRYPOINT ["clanker", "runner", "--data", "/var/lib/clanker-runner"]

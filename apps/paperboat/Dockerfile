# One Dockerfile builds every Paperboat release. build.sh supplies the build arguments;
# a release is a set of build arguments, not a branch.
#
#   VERSION        release version, reported by /api/version and the UI footer
#   FEATURE_LEVEL  1 for 1.0.0, 2 from 1.1.0 (aging, schema version 2)
#   FAULT          none, or inventory for the release that the rollback scenario uses
#   BASE_IMAGE     pinned runtime image; also reported by /api/version

ARG BASE_IMAGE
ARG GO_DIGEST=sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18

FROM golang:1.27.3-alpine@${GO_DIGEST} AS build
ARG VERSION
ARG FEATURE_LEVEL
ARG FAULT
ARG BASE_IMAGE
RUN test -n "${VERSION}" && test -n "${FEATURE_LEVEL}" && test -n "${FAULT}" \
  && test -n "${BASE_IMAGE}" \
  || (echo "VERSION, FEATURE_LEVEL, FAULT and BASE_IMAGE are required; use build.sh" >&2; exit 1)
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
COPY internal ./internal
ENV CGO_ENABLED=0
RUN pkg=github.com/statik/site-reliability-clanker/apps/paperboat/internal/buildinfo \
  && go build -trimpath -o /out/paperboat -ldflags "-s -w \
    -X ${pkg}.Version=${VERSION} \
    -X ${pkg}.FeatureLevel=${FEATURE_LEVEL} \
    -X ${pkg}.Fault=${FAULT} \
    -X ${pkg}.BaseImage=${BASE_IMAGE}" . \
  && mkdir /out/data

FROM ${BASE_IMAGE}
COPY --from=build /out/paperboat /paperboat
# An empty /data that the non-root user owns, so a new volume mounted there is writable.
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
EXPOSE 8080 8081
ENTRYPOINT ["/paperboat"]

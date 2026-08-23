# syntax=docker/dockerfile:1

# Gridiron 2000 (GoSX web app) container image.
#
# The builder compiles the server with `go build .` and then runs
# `gosx build --dev .` (the CLI version pinned to the go.mod module
# version). That emits dist/build.json plus hashed runtime and island
# assets, so the draft-room island hydrates in production. The dev asset
# tier is used because `gosx build --prod` requires a TinyGo toolchain;
# the dev tier serves the same island programs, only non-minified
# (~41 KB gzipped runtime), which is fine for an eight-manager league.
# Every user flow also works without JavaScript through plain HTML forms.

ARG APP_VERSION=dev
ARG GIT_SHA=unknown
ARG BUILD_DATE=unknown

FROM golang:1.26-bookworm AS builder

ARG APP_VERSION
ARG GIT_SHA
ARG BUILD_DATE

WORKDIR /src

# m31labs.dev/* modules resolve (via go-import redirects) to public GitHub
# repositories, so GOPRIVATE only needs to skip the module proxy/sumdb for
# them; go.sum already pins every checksum, so no sumdb network call happens.
ENV GOPRIVATE=github.com/odvcencio/*,github.com/M31-Labs/*,m31labs.dev/* \
    GOFLAGS=-mod=mod \
    CGO_ENABLED=0

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -trimpath -ldflags="-s -w -X main.appVersion=${APP_VERSION} -X main.appGitSHA=${GIT_SHA} -X main.appBuildDate=${BUILD_DATE}" -o /out/gridiron-2000 .

# Client assets: dist/ is excluded from the build context, so this always
# generates fresh island programs that match the GSX sources in the image.
# Keep the CLI version equal to the m31labs.dev/gosx version in go.mod.
# GOSX_SKIP_VERSION_CHECK stays set: the project standard is to skip the
# CLI's own self-reported-version check rather than depend on it matching
# exactly; the pinned CLI version below is what actually governs the build.
# v0.53.5 includes native same-origin return targets, fragment-aware managed
# navigation, the native document contract, nonce-aware navigation,
# accessible disclosure primitives, static bearer middleware, File/Files, and
# MaxActionBodyBytes plus shared request negotiation for managed actions. It
# also retains the last good declarative-region DOM across HTTP failures. The
# avatar route keeps its outer multipart envelope cap until the
# production consumer adopts a bounded-multipart contract.
RUN go install m31labs.dev/gosx/cmd/gosx@v0.53.5 && GOSX_SKIP_VERSION_CHECK=1 /go/bin/gosx build --dev .

# Runtime data directory. The PVC mount in Kubernetes covers /app/data in
# production; this pre-created, owner-only directory lets the same image
# run standalone (for example `docker run`) without a mounted volume.
RUN mkdir -p /out/data && chown 65532:65532 /out/data && chmod 700 /out/data

FROM gcr.io/distroless/static-debian12:nonroot AS runtime

ARG APP_VERSION
ARG GIT_SHA
ARG BUILD_DATE

LABEL org.opencontainers.image.title="Gridiron 2000" \
      org.opencontainers.image.version="${APP_VERSION}" \
      org.opencontainers.image.revision="${GIT_SHA}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.source="https://github.com/odvcencio/gridiron-2000"

WORKDIR /app

COPY --from=builder --chown=65532:65532 /out/gridiron-2000 /app/server
COPY --from=builder --chown=65532:65532 /src/app /app/app
COPY --from=builder --chown=65532:65532 /src/public /app/public
COPY --from=builder --chown=65532:65532 /src/config /app/config
COPY --from=builder --chown=65532:65532 /src/dist /app/dist
COPY --from=builder --chown=65532:65532 --chmod=700 /out/data /app/data

ENV GOSX_APP_ROOT=/app \
    PORT=8080

USER 65532:65532
EXPOSE 8080

ENTRYPOINT ["/app/server"]

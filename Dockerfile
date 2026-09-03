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
# v0.53.11-0.20260903011141-48af3189fe1f includes comment stripping inside
# .gsx markup at compile time; it is a maintenance patch on the v0.53 line,
# pinned here by pseudo-version because the v0.53.11 tag could not be pushed
# upstream yet.
# v0.53.10 includes event-mode live binds, attribute and class binds, region
# append and prepend, countdown retarget, cue mute, and hub backoff.
# v0.53.9 includes region-aware re-registration. It re-registers countdowns,
# watchers, and filters on the `gosx:region:after` event. It also keeps the
# shared interval alive across a rescan. The draft-room pick clock therefore
# keeps ticking after a pick swaps the draft region.
# v0.53.7 added opt-in return-target redirects with completion messages,
# fail-closed explicit redirects, and relocation-safe sibling file loading for
# trimpath production bundles, plus RenderProgramComponentNode for composing
# typed components into server-rendered fragments. It retains native same-origin
# return targets, fragment-aware managed navigation, the native document
# contract, nonce-aware navigation, accessible disclosure primitives, static
# bearer middleware, File/Files, MaxActionBodyBytes, shared request
# negotiation, and the last good declarative-region DOM across HTTP failures.
# The avatar route keeps its outer multipart envelope cap until the production
# consumer adopts a bounded-multipart contract.
RUN go install m31labs.dev/gosx/cmd/gosx@v0.53.11-0.20260903011141-48af3189fe1f && GOSX_SKIP_VERSION_CHECK=1 /go/bin/gosx build --dev .

# Prune build-only and duplicate assets from dist/ before it is COPY'd into
# the runtime stage (GC-3 app lane).
#
# Always removed: dist/server/app is a duplicate, unstripped server binary
# (the image ENTRYPOINT runs the stripped /out/gridiron-2000 binary, copied
# separately below as /app/server) and dist/assets/runtime/*.map are dev
# source maps that no emitted JS references via a sourceMappingURL trailer
# and that dist/build.json never names, so the runtime image can never
# resolve them.
#
# Removed ONLY when dist/build.json proves no island program shipped
# ("islands": null): the WASM runtime variants, both wasm_exec JS shims, and
# the scene3d/hls/stripe-bridge/relay/textlayout feature bundles. If islands
# is ever non-null, a live island program may load one of these assets by
# URL, so the guard below fails the build loudly instead of silently
# shipping a broken chunk. bootstrap.js and its .gz/.br sidecars are always
# kept: the lite/runtime fallback chain in the GoSX island/server code can
# still reach it, as are bootstrap-runtime, bootstrap-lite, and the
# bootstrap-feature-hubs/islands/engines/controllers bundles.
RUN set -eu; \
    RUNTIME_DIR=dist/assets/runtime; \
    BUILD_JSON=dist/build.json; \
    APP_BIN=dist/server/app; \
    before=$(du -sb dist | cut -f1); \
    app_bytes=0; \
    if [ -f "$APP_BIN" ]; then app_bytes=$(stat -c%s "$APP_BIN"); rm -f "$APP_BIN"; fi; \
    map_bytes=0; \
    for f in "$RUNTIME_DIR"/*.map; do \
        [ -e "$f" ] || continue; \
        map_bytes=$((map_bytes + $(stat -c%s "$f"))); \
        rm -f "$f"; \
    done; \
    echo "prune: dist/server/app=${app_bytes} bytes, *.map=${map_bytes} bytes (always-delete)"; \
    islands_line=$(grep -m1 '"islands"' "$BUILD_JSON" || true); \
    case "$islands_line" in \
        *'"islands": null'*) \
            guarded_bytes=0; \
            for pattern in 'gosx-runtime*.wasm' 'wasm_exec.*' 'standard-go-wasm_exec.*' 'bootstrap-feature-scene3d*' 'hls.min.*' 'stripe-bridge*' 'relay*' 'bootstrap-feature-textlayout*'; do \
                for f in "$RUNTIME_DIR"/$pattern; do \
                    [ -e "$f" ] || continue; \
                    guarded_bytes=$((guarded_bytes + $(stat -c%s "$f"))); \
                    rm -f "$f"; \
                done; \
            done; \
            echo "prune: guarded runtime assets=${guarded_bytes} bytes (islands: null confirmed)"; \
            ;; \
        *) \
            echo "REFUSING to prune $RUNTIME_DIR: dist/build.json does not report \"islands\": null (found: ${islands_line:-<missing key>}); a live island program may need the WASM runtime and feature bundles this step would delete. Update this guard (Dockerfile) before shipping island builds." >&2; \
            exit 1; \
            ;; \
    esac; \
    after=$(du -sb dist | cut -f1); \
    echo "prune: dist/ total before=${before} bytes after=${after} bytes reduced=$((before - after)) bytes"

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

# syntax=docker/dockerfile:1

# statrelay (cmd/statrelay): the shared Tank01 caching relay. A separate,
# smaller image from the main Gridiron 2000 app image (../Dockerfile) —
# statrelay is a dependency-free stdlib-only binary with no GoSX asset
# build step, so it does not need that Dockerfile's gosx build stage.
#
# Build from the repository root so the build context includes go.mod/
# go.sum and cmd/statrelay:
#   docker build -f deploy/statrelay.Dockerfile -t <tag> .

FROM golang:1.26-bookworm AS builder

WORKDIR /src

ENV GOPRIVATE=github.com/odvcencio/*,github.com/M31-Labs/*,m31labs.dev/* \
    GOFLAGS=-mod=mod \
    CGO_ENABLED=0

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/statrelay ./cmd/statrelay

RUN go build -trimpath -ldflags="-s -w" -o /out/statrelay ./cmd/statrelay

# Runtime cache directory. The PVC mount in Kubernetes covers this in
# production (see deploy/k8s/statrelay.yaml); pre-creating it here lets the
# same image run standalone (for example `docker run`) without a mounted
# volume.
RUN mkdir -p /out/data && chown 65532:65532 /out/data && chmod 700 /out/data

FROM gcr.io/distroless/static-debian12:nonroot AS runtime

WORKDIR /app

COPY --from=builder --chown=65532:65532 /out/statrelay /app/statrelay
COPY --from=builder --chown=65532:65532 --chmod=700 /out/data /app/data

ENV DATA_DIR=/app/data \
    LISTEN_ADDR=:8090

USER 65532:65532
EXPOSE 8090

ENTRYPOINT ["/app/statrelay"]

# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM tonistiigi/xx:1.10.0@sha256:40fe21f44bb8cddf1eb1226db190d0a984118f62ee02a91b77d99448d28264a6 AS xx

FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine3.24@sha256:b8c9b82affbbb82a020bbcf998dad2ba9516944ac7e883524642a481a9505e9c AS base
ENV GO111MODULE=on
ENV CGO_ENABLED=0
COPY --from=xx / /
RUN apk upgrade --no-cache \
    && apk add --update --upgrade --no-cache build-base coreutils git
WORKDIR /src

FROM base AS build
ARG TARGETPLATFORM
ARG VERSION=dev
RUN --mount=type=bind,target=/src,rw \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    xx-go build -trimpath -ldflags="-s -w -X deployah.dev/deployah/internal/cmd.version=${VERSION}" -o /usr/bin/deployah . \
    && xx-verify --static /usr/bin/deployah

FROM scratch AS binary
COPY --from=build /usr/bin/deployah /

FROM base AS releaser
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
WORKDIR /work
RUN --mount=from=binary,target=/build \
    --mount=type=bind,target=/src \
    mkdir -p /out \
    && cp /build/deployah /src/README.md /src/LICENSE . \
    # TODO: add zip packaging once windows targets are enabled.
    && tar -czvf "/out/deployah-${TARGETOS}-${TARGETARCH}${TARGETVARIANT}.tar.gz" \
         deployah README.md LICENSE \
    && cd /out \
    && sha256sum "deployah-${TARGETOS}-${TARGETARCH}${TARGETVARIANT}."* >> "SHA256SUMS"

FROM scratch AS artifact
COPY --from=releaser /out /

FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
RUN apk upgrade --no-cache \
    && apk add --update --upgrade --no-cache ca-certificates tzdata
COPY --from=binary /deployah /usr/bin/deployah
ENTRYPOINT ["deployah"]
CMD ["--help"]

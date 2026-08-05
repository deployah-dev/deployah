# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM tonistiigi/xx:1.9.0@sha256:c64defb9ed5a91eacb37f96ccc3d4cd72521c4bd18d5442905b95e2226b0e707 AS xx

FROM --platform=$BUILDPLATFORM golang:1.27rc2-alpine@sha256:dcbb18cc5fa1082364dc6aa95224b6b55429d09cbb9631a053d8064c1c367300 AS base
ENV GO111MODULE=on
ENV CGO_ENABLED=0
COPY --from=xx / /
RUN apk add --update --no-cache build-base coreutils git
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
    && sha256sum "deployah-${TARGETOS}-${TARGETARCH}${TARGETVARIANT}."* \
         > "deployah-${TARGETOS}-${TARGETARCH}${TARGETVARIANT}.sha256sum" \
    && sha256sum "deployah-${TARGETOS}-${TARGETARCH}${TARGETVARIANT}."* >> "SHA256SUMS"

FROM scratch AS artifact
COPY --from=releaser /out /

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
RUN apk add --update --no-cache ca-certificates tzdata
COPY --from=binary /deployah /usr/bin/deployah
ENTRYPOINT ["deployah"]
CMD ["--help"]

# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM tonistiigi/xx:1.6.1 AS xx

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS base
ENV GO111MODULE=on
ENV CGO_ENABLED=0
COPY --from=xx / /
RUN apk add --update --no-cache build-base coreutils git zip
WORKDIR /src

FROM base AS build
ARG TARGETPLATFORM
RUN --mount=type=bind,target=/src,rw \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    xx-go build -trimpath -ldflags="-s -w" -o /usr/bin/deployah . \
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
    && if [ "$TARGETOS" = "windows" ]; then \
         zip -q "/out/deployah-${TARGETOS}-${TARGETARCH}${TARGETVARIANT}.zip" \
           deployah README.md LICENSE; \
       else \
         tar -czvf "/out/deployah-${TARGETOS}-${TARGETARCH}${TARGETVARIANT}.tar.gz" \
           deployah README.md LICENSE; \
       fi \
    && cd /out \
    && sha256sum "deployah-${TARGETOS}-${TARGETARCH}${TARGETVARIANT}."* \
         > "deployah-${TARGETOS}-${TARGETARCH}${TARGETVARIANT}.sha256sum" \
    && sha256sum "deployah-${TARGETOS}-${TARGETARCH}${TARGETVARIANT}."* >> "SHA256SUMS"

FROM scratch AS artifact
COPY --from=releaser /out /

FROM alpine:3.22
RUN apk add --update --no-cache ca-certificates tzdata
COPY --from=binary /deployah /usr/bin/deployah
ENTRYPOINT ["deployah"]
CMD ["help"]

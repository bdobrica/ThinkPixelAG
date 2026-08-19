FROM --platform=$BUILDPLATFORM golang:1.26.6-alpine3.23@sha256:e57c41c1d5864341031181b0db34b9a537bb5773eb6428e4e5bdaea0f9135406 AS build

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG REVISION=unknown

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
    -trimpath -buildvcs=false \
    -ldflags "-s -w -X main.version=${VERSION} -X main.revision=${REVISION}" \
    -o /out/thinkpixelag ./cmd/thinkpixelag

FROM gcr.io/distroless/static-debian13:nonroot@sha256:f7f8f729987ad0fdf6b05eeeae94b26e6a0f613bdf46feea7fc40f7bd72953e6

ARG VERSION=dev
ARG REVISION=unknown

LABEL org.opencontainers.image.title="ThinkPixelAG" \
      org.opencontainers.image.description="Agent governance and lifecycle control plane" \
      org.opencontainers.image.source="https://github.com/bdobrica/ThinkPixelAG" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.revision=$REVISION

COPY --from=build --chown=65532:65532 /out/thinkpixelag /thinkpixelag

USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/thinkpixelag"]

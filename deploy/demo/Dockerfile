# Example-only provisioning/identity support. Not part of the AG release image.
FROM --platform=$BUILDPLATFORM golang:1.26.6-alpine3.23@sha256:e57c41c1d5864341031181b0db34b9a537bb5773eb6428e4e5bdaea0f9135406 AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal/ ./internal/
COPY test/operations/fixture/ ./test/operations/fixture/
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false -o /bootstrap ./test/operations/fixture
FROM quay.io/bdobrica/thinkpixelag@sha256:82ad793f6b19460e58aeb43203bfd2f8a3563bc2d8bf53a120e92dbafa78a53a AS ag
FROM openpolicyagent/opa:1.19.0-debug@sha256:ec3c7a29a21ce96d71231cb4befa2561205fe84e5a2dc3cc46ac7bc8bd21b3a4 AS opa
FROM postgres:18.4-alpine3.23@sha256:996d0920e4ff9df1fc19dacb904492f3c1ec0ec1cc338f0ad7123be7731c5f5e
COPY --from=build /bootstrap /bootstrap
COPY --from=opa /opa /usr/local/bin/opa
COPY --from=ag /thinkpixelag-migrate /thinkpixelag-migrate
COPY --from=ag /migrations /migrations
COPY policies/authorization.rego /policies/authorization.rego
COPY deploy/demo/provision.sh /provision.sh
RUN mkdir /state /trust && chown 65532:65532 /state /trust
USER 65532:65532
WORKDIR /
ENTRYPOINT ["/bootstrap"]

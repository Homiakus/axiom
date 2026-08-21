# Multi-stage minimal container for axiomgen CLI
FROM golang:1.26-alpine AS builder

WORKDIR /build

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=v0.0.0-dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -ldflags="-s -w -X 'main.Version=${VERSION}' -X 'main.Commit=${COMMIT}' -X 'main.BuildDate=${BUILD_DATE}'" \
    -o /out/axiomgen ./cmd/axiomgen

FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="Axiom Code Generator (axiomgen)" \
      org.opencontainers.image.description="Deterministic business logic schema compiler and code generator for Axiom" \
      org.opencontainers.image.source="https://github.com/Homiakus/axiom" \
      org.opencontainers.image.licenses="Apache-2.0"

USER nonroot:nonroot

WORKDIR /workspace

COPY --from=builder /out/axiomgen /usr/local/bin/axiomgen

ENTRYPOINT ["/usr/local/bin/axiomgen"]

FROM golang:1.25-alpine AS builder

WORKDIR /app

# Dependencies first so this layer is cached until go.mod/go.sum change.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/

# Git SHA, exposed at GET /version.
ARG VERSION=dev

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o go-api ./cmd/api

# Static binary only: no shell, no package manager.
FROM gcr.io/distroless/static-debian12

COPY --from=builder /app/go-api /go-api

# nobody; matches runAsUser in k8s/base/deployment.yaml.
USER 65534:65534

EXPOSE 8080

ENTRYPOINT ["/go-api"]

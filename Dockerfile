# Multi-stage build for the Forge kernel API. Distroless static, nonroot —
# same hardening as the ISF backend. Builds cmd/api (the request/response
# agent server). The PR-agent webhook entrypoint (cmd/server) gets its own
# image later when its context engine + git sandbox exist.
FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/api /app/api

EXPOSE 8090
USER nonroot:nonroot
ENTRYPOINT ["/app/api"]

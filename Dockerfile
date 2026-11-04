# 1) Build the React panel. Vite writes into internal/server/web/app so the Go
#    embed below picks the bundle up and the runtime image stays one binary.
FROM node:22-alpine AS web
WORKDIR /src
COPY web/package.json web/package-lock.json ./web/
RUN cd web && npm ci --no-audit --no-fund
COPY web ./web
COPY internal/server/web ./internal/server/web
RUN cd web && npm run build

FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/server/web/app ./internal/server/web/app
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /out/mailflow ./cmd/mailflow
RUN go run ./cmd/mailflow -check -stores stores

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/mailflow /app/mailflow
COPY stores /app/stores
ENV STORES_DIR=/app/stores LISTEN_ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/app/mailflow"]

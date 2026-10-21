FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /out/mailflow ./cmd/mailflow
RUN go run ./cmd/mailflow -check -stores stores

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/mailflow /app/mailflow
COPY stores /app/stores
ENV STORES_DIR=/app/stores LISTEN_ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/app/mailflow"]

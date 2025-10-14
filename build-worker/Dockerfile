# syntax=docker/dockerfile:1

FROM golang:1.22-alpine AS builder
WORKDIR /src

COPY go.mod .
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/build-worker .

FROM docker:24.0.7-cli
WORKDIR /app
COPY --from=builder /out/build-worker /usr/local/bin/build-worker
ENTRYPOINT ["/usr/local/bin/build-worker"]

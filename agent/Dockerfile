# syntax=docker/dockerfile:1

FROM golang:1.22-alpine AS builder
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/agent .

FROM alpine:3.19
RUN adduser -D -h /srv/agent app
USER app
WORKDIR /srv/agent

COPY --from=builder /out/agent /usr/local/bin/agent

ENTRYPOINT ["/usr/local/bin/agent"]

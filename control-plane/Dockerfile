# syntax=docker/dockerfile:1

FROM golang:1.22-alpine AS builder
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/control-plane .

FROM alpine:3.19
RUN adduser -D -h /srv/control-plane app
USER app
WORKDIR /srv/control-plane

COPY --from=builder /out/control-plane /usr/local/bin/control-plane

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/control-plane"]

FROM golang:1.10 AS builder
WORKDIR /go/src/github.com/cenkalti/tcpproxy
COPY . .
RUN CGO_ENABLED=0 go build -o /go/bin/tcpproxy cmd/tcpproxy/main.go

FROM alpine:3.5
COPY --from=builder /go/bin/tcpproxy /usr/local/bin/tcpproxy
ENTRYPOINT ["/usr/local/bin/tcpproxy"]

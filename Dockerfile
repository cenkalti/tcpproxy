FROM golang:1.21-alpine3.20 AS builder
WORKDIR /go/src/github.com/cenkalti/tcpproxy
COPY . .
ARG VERSION
RUN CGO_ENABLED=0 go build -o /go/bin/tcpproxy -ldflags="-s -w -X github.com/cenkalti/tcpproxy.Version=$VERSION" cmd/tcpproxy/main.go

FROM alpine:3.20
COPY --from=builder /go/bin/tcpproxy /usr/bin/tcpproxy
ENTRYPOINT ["/usr/bin/tcpproxy"]

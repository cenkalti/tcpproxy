# tcpproxy

Simple TCP proxy written in Go

[![Build Status](https://travis-ci.org/cenkalti/tcpproxy.svg?branch=master)](https://travis-ci.org/cenkalti/tcpproxy)
[![Coverage Status](https://coveralls.io/repos/github/cenkalti/tcpproxy/badge.svg?branch=master)](https://coveralls.io/github/cenkalti/tcpproxy)
![Docker Image Version (latest semver)](https://img.shields.io/docker/v/cenkalti/tcpproxy)

## Install

Build it yourself:
```go
$ go get github.com/cenkalti/tcpproxy/cmd/tcpproxy
```
or download pre-compiled binary from [releases page](https://github.com/cenkalti/tcpproxy/releases):
```sh
$ wget "https://github.com/cenkalti/tcpproxy/releases/download/v<VERSION>/tcpproxy"
```
or
pull pre-built [docker image](https://hub.docker.com/repository/docker/cenkalti/tcpproxy/):
```sh
$ docker pull cenk
```

## Usage

```
$ tcpproxy -h
usage: ./tcpproxy [options] listen_address remote_address
  -c duration
    	connect timeout (default 10s)
  -d	enable debug log
  -g duration
    	grace period in seconds before killing open connections (default 10s)
  -k duration
    	TCP keepalive period (default 1m0s)
  -m string
    	listen address for management interface
  -r duration
    	DNS resolve period (default 10s)
  -s string
    	file to save/load remote address to survive restarts
  -v	print version and exit
```

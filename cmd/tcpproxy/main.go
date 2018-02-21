package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cenkalti/tcpproxy"
)

var (
	mgmtListen     = flag.String("m", "", "listen address for management interface")
	gracePeriod    = flag.Duration("g", 10*time.Second, "grace period in seconds before killing open connections")
	connectTimeout = flag.Duration("c", 10*time.Second, "connect timeout")
	keepAlive      = flag.Duration("k", time.Minute, "TCP keepalive period")
	resolvePeriod  = flag.Duration("r", 10*time.Second, "DNS resolve period")
	statePath      = flag.String("s", "", "file to save/load remote address and grace period to survive restarts")
	version        = flag.Bool("v", false, "print version and exit")
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [options] listen_address remote_address\n", os.Args[0])
	flag.PrintDefaults()
}

func main() {
	flag.Usage = usage
	flag.Parse()

	if *version {
		fmt.Println(tcpproxy.Version)
		return
	}

	if len(flag.Args()) < 2 {
		log.Fatal("not enough args")
	}

	listenAddress := flag.Args()[0]
	remoteAddress := flag.Args()[1]

	p := tcpproxy.NewProxy(listenAddress, remoteAddress)

	p.MgmtListenAddr = *mgmtListen
	p.GracePeriod = *gracePeriod
	p.ConnectTimeout = *connectTimeout
	p.Keepalive = *keepAlive
	p.ResolvePeriod = *resolvePeriod
	p.StatePath = *statePath

	p.Run()
}

package main

import (
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
)

var (
	mgmt  = flag.String("m", "", "listen address for management interface")
	conns = make(map[net.Conn]struct{})
	m     sync.Mutex
)

func main() {
	flag.Parse()

	if len(flag.Args()) < 2 {
		log.Fatal("not enough args")
	}

	laddr := flag.Args()[0]
	raddr := flag.Args()[1]

	l, err := net.Listen("tcp", laddr)
	if err != nil {
		log.Fatalln("cannot listen", err)
	}

	if *mgmt != "" {
		go serveMgmt()
	}

	for {
		c, err := l.Accept()
		if err != nil {
			log.Fatalln("cannot accept", err)
		}
		log.Println("connected", c.RemoteAddr())
		m.Lock()
		conns[c] = struct{}{}
		m.Unlock()
		go proxy(c, raddr)
	}
}

func serveMgmt() {
	http.HandleFunc("/", handleIndex)
	http.ListenAndServe(*mgmt, nil)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("ok"))
}

func proxy(conn net.Conn, raddr string) {
	defer log.Println("disconnected", conn.RemoteAddr())
	defer conn.Close()
	rconn, err := net.Dial("tcp", raddr)
	if err != nil {
		log.Println("cannot dial", err)
		return
	}
	defer rconn.Close()
	go io.Copy(conn, rconn)
	io.Copy(rconn, conn)
}

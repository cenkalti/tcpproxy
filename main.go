package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
)

var (
	mgmt  = flag.String("m", "", "listen address for management interface")
	conns = make(map[*connPair]struct{})
	m     sync.Mutex
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [-m] listen_address remote_address\n", os.Args[0])
	flag.PrintDefaults()
}

type connPair struct {
	in, out net.Conn
}

func main() {
	flag.Usage = usage
	flag.Parse()

	if len(flag.Args()) < 2 {
		log.Fatal("not enough args")
	}

	laddr := flag.Args()[0]
	raddr := flag.Args()[1]

	l, err := net.Listen("tcp", laddr)
	if err != nil {
		log.Fatalln("cannot listen address:", err)
	}

	if *mgmt != "" {
		http.HandleFunc("/conns", handleConns)
		http.HandleFunc("/count", handleCount)
		go http.ListenAndServe(*mgmt, nil)
	}

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Fatalln("cannot accept inbound connection:", err)
		}
		log.Println("connected", conn.RemoteAddr())
		go proxy(conn, raddr)
	}
}

func handleConns(w http.ResponseWriter, r *http.Request) {
	var b bytes.Buffer
	m.Lock()
	for c := range conns {
		b.WriteString(c.in.RemoteAddr().String())
		b.WriteString(" -> ")
		if c.out != nil {
			b.WriteString(c.out.RemoteAddr().String())
		}
		b.WriteString("\n")
	}
	m.Unlock()
	w.Write(b.Bytes())
}

func handleCount(w http.ResponseWriter, r *http.Request) {
	var count int
	m.Lock()
	count = len(conns)
	m.Unlock()
	w.Write([]byte(strconv.Itoa(count)))
}

func proxy(conn net.Conn, raddr string) {
	defer log.Println("disconnected", conn.RemoteAddr())
	defer conn.Close()

	c := &connPair{in: conn}

	m.Lock()
	conns[c] = struct{}{}
	m.Unlock()

	defer func() {
		m.Lock()
		delete(conns, c)
		m.Unlock()
	}()

	rconn, err := net.Dial("tcp", raddr)
	if err != nil {
		log.Println("cannot connect to remote address:", err)
		return
	}
	defer rconn.Close()

	m.Lock()
	c.out = rconn
	m.Unlock()

	errc := make(chan error, 2)
	cp := func(dst io.Writer, src io.Reader) {
		_, err := io.Copy(dst, src)
		errc <- err
	}

	go cp(c.in, c.out)
	go cp(c.out, c.in)
	<-errc
	return
}

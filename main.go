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
	conns = make(map[*proxy]struct{})
	m     sync.Mutex
)

type proxy struct {
	in, out net.Conn
}

func newProxy(in net.Conn, raddr string) (*proxy, error) {
	out, err := net.Dial("tcp", raddr)
	if err != nil {
		return nil, err
	}
	return &proxy{in, out}, nil
}

func (c *proxy) Close() {
	go c.in.Close()
	go c.out.Close()
}

func (c *proxy) Run() error {
	defer log.Println("disconnected", c.in.RemoteAddr())
	defer c.Close()

	errc := make(chan error, 2)
	cp := func(dst io.Writer, src io.Reader) {
		_, err := io.Copy(dst, src)
		errc <- err
	}

	go cp(c.in, c.out)
	go cp(c.out, c.in)
	return <-errc
}

func main() {
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
		go serveMgmt(*mgmt)
	}

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Fatalln("cannot accept inbound connection:", err)
		}
		log.Println("connected", conn.RemoteAddr())
		p, err := newProxy(conn, raddr)
		if err != nil {
			conn.Close()
			log.Println("cannot connect to remote address:", err)
			continue
		}
		go p.Run()
	}
}

func serveMgmt(addr string) {
	http.HandleFunc("/conns", handleConns)
	http.ListenAndServe(addr, nil)
}

func handleConns(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("ok"))
}

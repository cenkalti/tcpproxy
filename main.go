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
	"time"
)

var (
	mgmt  = flag.String("m", "", "listen address for management interface")
	grace = flag.Int64("g", 30, "grace period in seconds before killing open connections")
	conns = make(map[*connPair]struct{})
	m     sync.Mutex
	wg    sync.WaitGroup
	raddr string
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
	raddr = flag.Args()[1]

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
		go proxy(conn)
	}
}

func serveMgmt(addr string) {
	http.HandleFunc("/conns", handleConns)
	http.HandleFunc("/conns/count", handleCount)
	http.HandleFunc("/raddr", handleRaddr)
	err := http.ListenAndServe(*mgmt, nil)
	if err != nil {
		log.Fatal(err)
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

func handleRaddr(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		m.Lock()
		addr := raddr
		m.Unlock()
		w.Write([]byte(addr))
	case http.MethodPut:
		var buf bytes.Buffer
		_, err := buf.ReadFrom(http.MaxBytesReader(w, r.Body, 259)) // 253 for host, 1 for colon, 5 for port
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		addr := buf.String()
		_, _, err = net.SplitHostPort(addr)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		m.Lock()
		log.Println("changing remote address to", addr)
		log.Println("waiting for open connections to shutdown gracefully for", *grace, "seconds")
		if waitTimeout(&wg, time.Duration(*grace)*time.Second) {
			log.Println("some connections didn't shutdown gracefully, killing them.")
			for c := range conns {
				if c.out != nil {
					c.out.Close()
				}
			}
		}
		raddr = addr
		log.Println("remote adress is changed to", addr)
		m.Unlock()
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func proxy(conn net.Conn) {
	defer log.Println("disconnected", conn.RemoteAddr())

	c := &connPair{in: conn}

	m.Lock()
	conns[c] = struct{}{}
	addr := raddr
	m.Unlock()

	defer func() {
		m.Lock()
		delete(conns, c)
		m.Unlock()
	}()

	defer conn.Close()

CONNECT:
	rconn, err := net.Dial("tcp", addr)
	if err != nil {
		log.Println("cannot connect to remote address:", err)
		return
	}

	m.Lock()
	if addr != raddr {
		// raddr may change while we are connecting to remote address.
		// if changed, close the current remote connection and connect to new address.
		rconn.Close()
		addr = raddr
		m.Unlock()
		goto CONNECT
	}
	c.out = rconn
	wg.Add(1)
	m.Unlock()

	defer wg.Done()
	defer rconn.Close()

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

func waitTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	c := make(chan struct{})
	go func() {
		defer close(c)
		wg.Wait()
	}()
	select {
	case <-c:
		return false // completed normally
	case <-time.After(timeout):
		return true // timed out
	}
}

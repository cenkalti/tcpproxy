package main

import (
	"bytes"
	"encoding/json"
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
	// contains info about the target server
	state serverState

	// save state in this file to survive restarts
	statePath string

	// listen http on this address for management interface
	mgmtListenAddr string

	// seconds to wait before killing open connections on remote address switch
	gracePeriod time.Duration

	// time to wait when connecting the server
	connectTimeout time.Duration

	// tcp keepalive period
	keepalive time.Duration

	// holds open connections
	conns = make(map[*connPair]struct{})

	// protects RemoteAddress and conns
	m sync.Mutex

	// for waiting open connections to shutdown gracefully
	wg sync.WaitGroup
)

type serverState struct {
	// proxy incoming connections to this address
	RemoteAddress string
}

func (s serverState) String() string {
	return fmt.Sprintf("RemoteAddress: %#v", s.RemoteAddress)
}

func (s *serverState) load() {
	if statePath == "" {
		return
	}
	f, err := os.Open(statePath)
	if os.IsNotExist(err) {
		s.save()
		return
	}
	if err != nil {
		log.Fatalln("cannot open state file:", err)
	}
	size, err := f.Seek(0, os.SEEK_END)
	if err != nil {
		log.Fatalln("cannot seek state file:", err)
	}
	_, err = f.Seek(0, os.SEEK_SET)
	if err != nil {
		log.Fatalln("cannot seek state file:", err)
	}
	if size == 0 {
		f.Close()
		s.save()
		return
	}
	err = json.NewDecoder(f).Decode(&state)
	if err != nil {
		log.Fatalln("cannot read state file:", err)
	}
	log.Println("state is loaded:", state)
	f.Close()
}

func (s serverState) save() {
	if statePath == "" {
		return
	}
	f, err := os.Create(statePath)
	if err != nil {
		log.Fatalln("cannot open state file:", err)
	}
	defer f.Close()
	err = json.NewEncoder(f).Encode(s)
	if err != nil {
		log.Println("cannot write state file:", err)
		return
	}
	err = f.Sync()
	if err != nil {
		log.Println("cannot sync state file:", err)
		return
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [options] listen_address remote_address\n", os.Args[0])
	flag.PrintDefaults()
}

type connPair struct {
	in, out net.Conn
}

func main() {
	flag.StringVar(&mgmtListenAddr, "m", "", "listen address for management interface")
	flag.DurationVar(&gracePeriod, "g", 10*time.Second, "grace period in seconds before killing open connections")
	flag.DurationVar(&connectTimeout, "c", 10*time.Second, "connect timeout")
	flag.DurationVar(&keepalive, "k", time.Minute, "TCP keepalive period")
	flag.StringVar(&statePath, "s", "", "file to save/load remote address and grace period to survive restarts")
	flag.Parse()

	if len(flag.Args()) < 2 {
		log.Fatal("not enough args")
	}

	laddr := flag.Args()[0]
	state.RemoteAddress = flag.Args()[1]

	state.load()

	l, err := net.Listen("tcp", laddr)
	if err != nil {
		log.Fatalln("cannot listen address:", err)
	}

	if mgmtListenAddr != "" {
		go serveMgmt(mgmtListenAddr)
	}

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Println("cannot accept inbound connection:", err)
			continue
		}
		log.Println("connected", conn.RemoteAddr())
		go proxy(conn)
	}
}

func serveMgmt(addr string) {
	http.HandleFunc("/conns", handleConns)
	http.HandleFunc("/conns/count", handleCount)
	http.HandleFunc("/raddr", handleRaddr)
	err := http.ListenAndServe(addr, nil)
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
		b.WriteString(c.in.LocalAddr().String())
		b.WriteString(" -> ")
		if c.out != nil {
			b.WriteString(c.out.LocalAddr().String())
			b.WriteString(" -> ")
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
		addr := state.RemoteAddress
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
		log.Println("waiting for open connections to shutdown gracefully for", gracePeriod, "seconds")
		if waitTimeout(&wg, gracePeriod) {
			log.Println("some connections didn't shutdown gracefully, killing them.")
			for c := range conns {
				if c.out != nil {
					c.out.Close()
				}
			}
		}
		state.RemoteAddress = addr
		state.save()
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
	addr := state.RemoteAddress
	m.Unlock()

	defer func() {
		m.Lock()
		delete(conns, c)
		m.Unlock()
	}()

	defer conn.Close()

	setKeepAlive(conn)

CONNECT:
	rconn, err := net.DialTimeout("tcp", addr, connectTimeout)
	if err != nil {
		log.Println("cannot connect to remote address:", err)
		return
	}

	m.Lock()
	if addr != state.RemoteAddress {
		// raddr may change while we are connecting to remote address.
		// if changed, close the current remote connection and connect to new address.
		rconn.Close()
		addr = state.RemoteAddress
		m.Unlock()
		goto CONNECT
	}
	c.out = rconn
	wg.Add(1)
	m.Unlock()

	defer wg.Done()
	defer rconn.Close()

	setKeepAlive(rconn)

	errc := make(chan error, 2)
	go copyStream(c.in, c.out, errc)
	go copyStream(c.out, c.in, errc)
	<-errc
	return
}

func setKeepAlive(conn net.Conn) {
	tconn, ok := conn.(*net.TCPConn)
	if !ok {
		log.Println("cannot set TCP keepalive: not TCP connection")
		return
	}
	err := tconn.SetKeepAlivePeriod(keepalive)
	if err != nil {
		log.Println("cannot set keepalive period:", err)
	}
	err = tconn.SetKeepAlive(true)
	if err != nil {
		log.Println("cannot set keepalive:", err)
	}
}

func copyStream(dst io.Writer, src io.Reader, errc chan<- error) {
	_, err := io.Copy(dst, src)
	errc <- err
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

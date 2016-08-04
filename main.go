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
	stateFile string

	// listen http on this address for management interface
	mgmtListenAddr string

	// holds open connections
	conns = make(map[*connPair]struct{})

	// protects raddr, gracePeriod and conns
	m sync.Mutex

	// for waiting open connections to shutdown gracefully
	wg sync.WaitGroup
)

type serverState struct {
	// proxy incoming connections to this address
	RemoteAddress string

	// seconds to wait before killing open connections on remote address switch
	GracePeriod int64

	f *os.File
}

func (s serverState) String() string {
	return fmt.Sprintf("RemoteAddress: %#v, GracePeriod: %#v", s.RemoteAddress, s.GracePeriod)
}

func (s *serverState) load() {
	if stateFile == "" {
		return
	}
	var err error
	s.f, err = os.Open(stateFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Fatalln("cannot open state file:", err)
		}
		log.Println("state file not exist, will create new one at", stateFile)
		s.f, err = os.Create(stateFile)
		if err != nil {
			log.Fatalln("cannot create state file:", err)
		}
		err = json.NewEncoder(s.f).Encode(s)
		if err != nil {
			log.Fatalln("cannot write state file:", err)
		}
		err = s.f.Sync()
		if err != nil {
			log.Fatalln("cannot sync state file:", err)
		}
		return
	}
	err = json.NewDecoder(s.f).Decode(&state)
	if err != nil {
		log.Fatalln("cannot read state file:", err)
	}
	log.Println("state is loaded:", state)
}

func (s serverState) save() {
	if s.f == nil {
		return
	}
	err := s.f.Truncate(0)
	if err != nil {
		log.Println("cannot truncate state file:", err)
		return
	}
	err = json.NewEncoder(s.f).Encode(s)
	if err != nil {
		log.Println("cannot write state file:", err)
		return
	}
	err = s.f.Sync()
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
	flag.Int64Var(&state.GracePeriod, "g", 30, "grace period in seconds before killing open connections")
	flag.StringVar(&stateFile, "s", "", "file to save/load remote address and grace period to survive restarts")
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
	http.HandleFunc("/grace", handleGrace)
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
		log.Println("waiting for open connections to shutdown gracefully for", state.GracePeriod, "seconds")
		if waitTimeout(&wg, time.Duration(state.GracePeriod)*time.Second) {
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

func handleGrace(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		m.Lock()
		grace := strconv.FormatInt(state.GracePeriod, 10)
		m.Unlock()
		w.Write([]byte(grace))
	case http.MethodPut:
		var buf bytes.Buffer
		_, err := buf.ReadFrom(http.MaxBytesReader(w, r.Body, 20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		grace, err := strconv.ParseInt(buf.String(), 10, 64)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		m.Lock()
		log.Println("changing grace period to", grace)
		state.GracePeriod = grace
		state.save()
		log.Println("grace period is changed to", grace)
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

CONNECT:
	rconn, err := net.Dial("tcp", addr)
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

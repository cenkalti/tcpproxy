package tcpproxy

import (
	"bytes"
	"log"
	"net"
	"net/http"
	"strconv"
)

func (p *Proxy) serveMgmt() {
	http.HandleFunc("/conns", p.handleConns)
	http.HandleFunc("/conns/count", p.handleCount)
	http.HandleFunc("/raddr", p.handleRaddr)
	err := http.Serve(p.mgmtListener, nil)
	if err != nil {
		select {
		case <-p.shutdown:
			return
		default:
		}
		log.Fatal(err)
	}
}

func (p *Proxy) handleConns(w http.ResponseWriter, r *http.Request) {
	var b bytes.Buffer
	handleConn := func(key, value interface{}) bool {
		c := key.(*proxyConn)
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
		return true
	}
	p.conns.Range(handleConn)
	_, _ = w.Write(b.Bytes())
}

func (p *Proxy) handleCount(w http.ResponseWriter, r *http.Request) {
	var count int
	handleConn := func(key, value interface{}) bool {
		count++
		return true
	}
	p.conns.Range(handleConn)
	_, _ = w.Write([]byte(strconv.Itoa(count)))
}

func (p *Proxy) handleRaddr(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		addr := p.GetRemoteAddress()
		_, _ = w.Write([]byte(addr))
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
		p.SetRemoteAddress(addr)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

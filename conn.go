package tcpproxy

import (
	"io"
	"log"
	"net"
	"time"
)

type proxyConn struct {
	in, out net.Conn
}

func newProxyConn(in net.Conn) *proxyConn {
	return &proxyConn{
		in: in,
	}
}

func (p *proxyConn) copyStream() <-chan error {
	errc := make(chan error, 2)
	cp := func(dst io.Writer, src io.Reader) {
		_, err := io.Copy(dst, src)
		errc <- err
	}
	go cp(p.in, p.out)
	go cp(p.out, p.in)
	return errc
}

func setKeepAlive(conn net.Conn, keepAlivePeriod time.Duration) {
	tconn, ok := conn.(*net.TCPConn)
	if !ok {
		log.Println("cannot set TCP keepalive: not TCP connection")
		return
	}
	err := tconn.SetKeepAlivePeriod(keepAlivePeriod)
	if err != nil {
		log.Println("cannot set keepalive period:", err)
	}
	err = tconn.SetKeepAlive(true)
	if err != nil {
		log.Println("cannot set keepalive:", err)
	}
}

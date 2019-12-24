package tcpproxy

import (
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	proxyPort    = "8000"
	servicePort  = "9000"
	servicePort2 = "9001"
	mgmtPort     = "8001"
	testString   = "hello world"
)

func init() {
	// enable debug messages
	t := true
	debugLog = &t
}

func shutdownProxy(t *testing.T, p *Proxy) {
	t.Helper()
	err := p.Shutdown()
	if err != nil {
		t.Fatal(err)
	}
}

func TestProxyTCP(t *testing.T) {
	// start a proxy server
	p := NewProxy("0.0.0.0:"+proxyPort, "127.0.0.1:"+servicePort)
	go p.Run()
	<-p.Ready
	defer shutdownProxy(t, p)

	// start a tcp echo server
	echo := echoServer(t, "0.0.0.0:"+servicePort)
	defer echo.Close()

	// connect to the echo server through proxy
	conn, err := net.Dial("tcp", "127.0.0.1:"+proxyPort)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// send a string through the connection
	_, err = conn.Write([]byte(testString))
	if err != nil {
		t.Fatal(err)
	}

	// must receive the same string
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(testString) {
		t.FailNow()
	}
	if string(buf[:n]) != testString {
		t.FailNow()
	}
}

// echoServer is a tcp server that responds with the received message.
func echoServer(t *testing.T, addr string) net.Listener {
	t.Helper()
	echo, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		for {
			conn, err2 := echo.Accept()
			if err2 != nil {
				break
			}
			go func(conn net.Conn) {
				buf := make([]byte, 1024)
				size, err3 := conn.Read(buf)
				if err3 != nil {
					return
				}
				data := buf[:size]
				conn.Write(data)
			}(conn)
		}
	}()
	return echo
}

func TestProxyChangeRemoteAddress(t *testing.T) {
	// start tcp proxy
	p := NewProxy("0.0.0.0:"+proxyPort, "127.0.0.1:"+servicePort)
	p.GracePeriod = 0
	p.MgmtListenAddr = "0.0.0.0:" + mgmtPort
	go p.Run()
	<-p.Ready
	defer shutdownProxy(t, p)

	// start echo tcp server
	echo := echoServer(t, "0.0.0.0:"+servicePort)
	defer echo.Close()

	// connect to echo server through proxy
	conn, err := net.Dial("tcp", "127.0.0.1:"+proxyPort)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// send a string through the proxy
	_, err = conn.Write([]byte(testString))
	if err != nil {
		t.Fatal(err)
	}

	// must receive the same string
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(testString) {
		t.FailNow()
	}
	if string(buf[:n]) != testString {
		t.FailNow()
	}

	// change remote address of the proxy
	data := strings.NewReader("127.0.0.1:" + servicePort2)
	req, err := http.NewRequest(http.MethodPut, "http://127.0.0.1:"+mgmtPort+"/raddr", data)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.FailNow()
	}

	// conn must be closed after remote address has been changed
	conn.SetReadDeadline(time.Now().Add(time.Second))
	_, err = conn.Read(buf)
	if err2, ok := err.(net.Error); ok && err2.Timeout() {
		t.FailNow()
	}
	conn.Close()

	// start another server on new port
	echo.Close()
	echo2 := echoServer(t, "0.0.0.0:"+servicePort2)
	defer echo2.Close()

	// connect to proxy again, new connection must be made to the second echo server
	conn, err = net.Dial("tcp", "127.0.0.1:"+proxyPort)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// send a string through the proxy, 2nd echo server this time
	_, err = conn.Write([]byte(testString))
	if err != nil {
		t.Fatal(err)
	}

	// must receive the same string
	buf = make([]byte, 1024)
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(testString) {
		t.FailNow()
	}
	if string(buf[:n]) != testString {
		t.FailNow()
	}
}

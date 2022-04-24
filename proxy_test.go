package tcpproxy

import (
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
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

const (
	numClients  = 50
	numRequests = 5000
)

func TestProxyTCP(t *testing.T) {
	// start a proxy server
	p := NewProxy("0.0.0.0:"+proxyPort, "127.0.0.1:"+servicePort)
	go p.Run()
	<-p.Ready
	defer shutdownProxy(t, p)

	// start a tcp echo server
	closeServer := echoServer(t, "0.0.0.0:"+servicePort)
	defer closeServer()

	errC := make(chan error, numClients)
	var wg sync.WaitGroup
	wg.Add(numClients)
	for i := 0; i < numClients; i++ {
		go func() {
			err := echoClient()
			if err != nil {
				errC <- err
			}
			wg.Done()
		}()
	}
	wg.Wait()

	select {
	case err := <-errC:
		t.Fatalf("client error: %s", err.Error())
	default:
	}
}

func echoClient() error {
	// connect to the echo server through proxy
	conn, err := net.Dial("tcp", "127.0.0.1:"+proxyPort)
	if err != nil {
		return err
	}
	defer conn.Close()

	// repeatedly send requests and check response
	for i := 0; i < numRequests; i++ {
		err = writeAndRead(conn)
		if err != nil {
			return err
		}
	}
	return nil
}

func writeAndRead(conn net.Conn) error {
	// send a string through the connection
	_, err := conn.Write([]byte(testString))
	if err != nil {
		return err
	}

	// must receive the same string
	buf := make([]byte, 1024)
	n, err := io.ReadFull(conn, buf[:len(testString)])
	if err != nil {
		return err
	}
	if string(buf[:n]) != testString {
		return errors.New("invalid value")
	}
	return nil
}

// echoServer is a tcp server that responds with the received message.
func echoServer(t *testing.T, addr string) func() {
	t.Helper()
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() {
		for {
			conn, err2 := l.Accept()
			if err2 != nil {
				select {
				case <-closed:
					return
				default:
				}
				t.Logf("echo accept error: %s", err2.Error())
				continue
			}
			go func(conn net.Conn) {
				buf := make([]byte, 1024)
				for {
					size, err3 := conn.Read(buf)
					if err3 == io.EOF {
						return
					}
					if err3 != nil {
						t.Logf("echo server read error: %s", err3.Error())
						return
					}
					data := buf[:size]
					_, err3 = conn.Write(data)
					if err3 != nil {
						t.Logf("echo server write error: %s", err3.Error())
						return
					}
				}
			}(conn)
		}
	}()
	return func() {
		select {
		case <-closed:
		default:
			close(closed)
			l.Close()
		}
	}
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
	closeServer := echoServer(t, "0.0.0.0:"+servicePort)
	defer closeServer()

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
	err = conn.SetReadDeadline(time.Now().Add(time.Second))
	if err != nil {
		t.FailNow()
	}
	_, err = conn.Read(buf)
	if err2, ok := err.(net.Error); ok && err2.Timeout() {
		t.FailNow()
	}
	conn.Close()

	// start another server on new port
	closeServer()
	closeServer2 := echoServer(t, "0.0.0.0:"+servicePort2)
	defer closeServer2()

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

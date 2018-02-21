package tcpproxy

import (
	"log"
	"net"
	"sync"
	"time"
)

type Proxy struct {
	// enable debug log
	DebugLog bool

	// listen address for incoming connections
	listenAddress string

	// proxy incoming connections to here
	remoteAddress string

	remote *remote

	// save state in this file to survive restarts
	StatePath string

	// listen http on this address for management interface
	MgmtListenAddr string

	// seconds to wait before killing open connections on remote address switch
	GracePeriod time.Duration

	// time to wait when connecting the server
	ConnectTimeout time.Duration

	// tcp keepalive period
	Keepalive time.Duration

	// holds open connections
	conns sync.Map

	// when should we resolve again?
	ResolvePeriod time.Duration

	// channel is closed when proxy is ready to accept connections
	Ready chan struct{}

	// listens RemoteAddress for incoming connections
	proxyListener net.Listener

	// listens HTTP requests for management interface
	mgmtListener net.Listener

	// for waiting active connections on Shutdown
	wg sync.WaitGroup

	// will be closed when Shutdown is called
	shutdown chan struct{}

	m sync.Mutex
}

func NewProxy(listenAddress, remoteAddress string) *Proxy {
	return &Proxy{
		listenAddress:  listenAddress,
		remoteAddress:  remoteAddress,
		GracePeriod:    10 * time.Second,
		ConnectTimeout: 10 * time.Second,
		Keepalive:      60 * time.Second,
		ResolvePeriod:  10 * time.Second,
		Ready:          make(chan struct{}),
		shutdown:       make(chan struct{}),
	}
}

func (p *Proxy) Run() {
	p.remote = newRemote(p.remoteAddress, p.ResolvePeriod)
	p.loadState()

	var err error
	p.proxyListener, err = net.Listen("tcp", p.listenAddress)
	if err != nil {
		log.Fatalln("cannot listen address:", err)
	}

	if p.MgmtListenAddr != "" {
		p.mgmtListener, err = net.Listen("tcp", p.MgmtListenAddr)
		if err != nil {
			log.Fatalln("cannot listen address:", err)
		}
		go p.serveMgmt()
	}

	close(p.Ready)
	for {
		conn, err := p.proxyListener.Accept()
		if err != nil {
			select {
			case <-p.shutdown:
				return
			default:
			}
			log.Println("cannot accept inbound connection:", err)
			continue
		}
		go p.handleConn(conn)
	}
}

func (p *Proxy) Shutdown() error {
	close(p.shutdown)
	err := p.proxyListener.Close()
	if err != nil {
		return err
	}
	if p.mgmtListener != nil {
		err = p.mgmtListener.Close()
		if err != nil {
			return err
		}
	}
	p.wg.Wait()
	return nil
}

func (p *Proxy) handleConn(in net.Conn) {
	p.wg.Add(1)
	debugln("connected", in.RemoteAddr())
	c := newProxyConn(in)
	p.conns.Store(c, nil)
	p.proxyConn(c)
	p.conns.Delete(c)
	in.Close()
	debugln("disconnected", in.RemoteAddr())
	p.wg.Done()
}

func (p *Proxy) proxyConn(c *proxyConn) {
	setKeepAlive(c.in, p.Keepalive)

	err := p.connectRemote(c)
	if err != nil {
		log.Println("cannot connect remote address:", err)
		return
	}
	defer c.out.Close()

	setKeepAlive(c.out, p.Keepalive)

	<-c.copyStream()
}

func (p *Proxy) connectRemote(c *proxyConn) error {
	connectAddr, err := p.remote.getIP()
	if err != nil {
		return err
	}
	for {
		rconn, err := net.DialTimeout("tcp", connectAddr, p.ConnectTimeout)
		if err != nil {
			return err
		}
		addr, err := p.remote.getIP()
		if err != nil {
			return err
		}
		if addr != connectAddr {
			// Remote address has changed while we are connecting to it.
			// If changed, close the current remote connection and connect to new address.
			rconn.Close()
			connectAddr = addr
			continue
		}
		c.out = rconn
		return nil
	}
}

func (p *Proxy) GetRemoteAddress() string {
	return p.remote.getAddr()
}

func (p *Proxy) SetRemoteAddress(newAddr string) {
	log.Println("changing remote address to", newAddr)
	log.Println("old remote address was", p.remote.getAddr())

	p.m.Lock()
	defer p.m.Unlock()

	p.remote.setAddr(newAddr)
	p.saveState()

	log.Println("remote adress has been changed to", newAddr)

	go p.killOldConns()
}

func (p *Proxy) killOldConns() {
	log.Println("waiting for open connections to shutdown gracefully for", p.GracePeriod)
	time.Sleep(p.GracePeriod)

	addr := p.remote.getAddr()

	var count int
	handleConn := func(key, value interface{}) bool {
		c := key.(*proxyConn)
		if c.out != nil {
			if c.out.RemoteAddr().String() != addr {
				c.out.Close()
				count++
			}
		}
		return true
	}
	p.conns.Range(handleConn)

	log.Println("killed", count, "old connections")
}

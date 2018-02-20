package tcpproxy

import (
	"net"
	"sync"
	"time"
)

type remote struct {
	addr            string
	ttl             time.Duration
	resolvedAddress string
	resolveTime     time.Time
	sync.RWMutex
}

func newRemote(addr string, ttl time.Duration) *remote {
	return &remote{
		addr: addr,
		ttl:  ttl,
	}
}

func (r *remote) getAddr() string {
	r.RLock()
	addr := r.addr
	r.RUnlock()
	return addr
}

func (r *remote) setAddr(addr string) {
	r.Lock()
	r.addr = addr
	r.resolvedAddress = ""
	r.Unlock()
}

func (r *remote) getIP() (string, error) {
	r.Lock()
	defer r.Unlock()

	if time.Since(r.resolveTime) < r.ttl && r.resolvedAddress != "" {
		return r.resolvedAddress, nil
	}
	host, port, err := net.SplitHostPort(r.addr)
	if err != nil {
		return "", err
	}
	ip, err := getIPString(host)
	if err != nil {
		return "", err
	}
	r.resolvedAddress = net.JoinHostPort(ip, port)
	r.resolveTime = time.Now()
	return r.resolvedAddress, nil
}

func getIPString(host string) (string, error) {
	ip := net.ParseIP(host)
	if ip != nil {
		return ip.String(), nil
	}
	addrs, err := net.LookupHost(host)
	if err != nil {
		return "", err
	}
	return addrs[0], nil
}

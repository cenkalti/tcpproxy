package tcpproxy

import (
	"encoding/json"
	"io"
	"log"
	"os"
)

// for marshalling/unmarshalling json
type serverState struct {
	// proxy incoming connections to this address
	RemoteAddress string
}

func (p *Proxy) loadState() {
	if p.StatePath == "" {
		return
	}
	f, err := os.Open(p.StatePath)
	if os.IsNotExist(err) {
		p.saveState()
		return
	}
	if err != nil {
		log.Fatalln("cannot open state file:", err)
	}
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		log.Fatalln("cannot seek state file:", err)
	}
	_, err = f.Seek(0, io.SeekStart)
	if err != nil {
		log.Fatalln("cannot seek state file:", err)
	}
	if size == 0 {
		f.Close()
		p.saveState()
		return
	}
	var s serverState
	err = json.NewDecoder(f).Decode(&s)
	if err != nil {
		log.Fatalln("cannot read state file:", err)
	}
	p.remote.setAddr(s.RemoteAddress)
	log.Println("loaded remote address:", s.RemoteAddress)
	f.Close()
}

func (p *Proxy) saveState() {
	if p.StatePath == "" {
		return
	}
	f, err := os.Create(p.StatePath)
	if err != nil {
		log.Fatalln("cannot open state file:", err)
	}
	defer f.Close()
	s := serverState{RemoteAddress: p.remote.getAddr()}
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

package tcpproxy

import (
	"flag"
	"log"
)

var debugLog = flag.Bool("d", false, "enable debug log")

func debugln(msg ...interface{}) {
	if *debugLog {
		log.Println(msg...)
	}
}

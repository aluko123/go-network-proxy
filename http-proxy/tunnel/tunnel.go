package tunnel

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// HandleTunneling handles HTTPS CONNECT requests for tunneling
func HandleTunneling(w http.ResponseWriter, r *http.Request) {
	destConn, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer destConn.Close()
	w.WriteHeader(http.StatusOK)

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	srcConn, _, err := hj.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	}
	defer srcConn.Close()

	srcConnStr := fmt.Sprintf("%s->%s", srcConn.LocalAddr().String(), srcConn.RemoteAddr().String())
	destConnStr := fmt.Sprintf("%s->%s", destConn.LocalAddr().String(), destConn.RemoteAddr().String())

	log.Printf("%s - %s - %s\n", r.Proto, r.Method, r.Host)
	log.Printf("srcConn: %s - destConn: %s\n", srcConnStr, destConnStr)

	var wg sync.WaitGroup
	wg.Add(2)

	go transfer(&wg, destConn, srcConn, destConnStr, srcConnStr)
	go transfer(&wg, srcConn, destConn, srcConnStr, destConnStr)
	wg.Wait()
}

// transfer copies data between connections bidirectionally
func transfer(wg *sync.WaitGroup, destination io.Writer, source io.Reader, destName, srcName string) {
	defer wg.Done()
	written, err := io.Copy(destination, source)
	if err != nil {
		fmt.Printf("Error transferring data from %s to %s: %v\n", srcName, destName, err)
	}
	fmt.Printf("Transferred %d bytes from %s to %s\n", written, srcName, destName)
}

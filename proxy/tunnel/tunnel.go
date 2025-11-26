package tunnel

import (
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// Config holds tunnel configuration
type Config struct {
	DialTimeout time.Duration
}

// DefaultConfig returns the default tunnel configuration
func DefaultConfig() Config {
	return Config{
		DialTimeout: 10 * time.Second,
	}
}

var config = DefaultConfig()

// SetConfig updates the tunnel configuration
func SetConfig(c Config) {
	config = c
}

// HandleTunneling handles HTTPS CONNECT requests for tunneling
func HandleTunneling(w http.ResponseWriter, r *http.Request) {
	destConn, err := net.DialTimeout("tcp", r.Host, config.DialTimeout)
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

	var wg sync.WaitGroup
	wg.Add(2)

	go transfer(&wg, destConn, srcConn)
	go transfer(&wg, srcConn, destConn)
	wg.Wait()
}

// transfer copies data between connections bidirectionally
func transfer(wg *sync.WaitGroup, destination io.Writer, source io.Reader) {
	defer wg.Done()
	io.Copy(destination, source)
}

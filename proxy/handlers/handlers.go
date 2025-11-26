package handlers

import (
	"io"
	"net"
	"net/http"
	"time"
)

// Config holds HTTP handler configuration
type Config struct {
	DialTimeout     time.Duration
	IdleConnTimeout time.Duration
}

// DefaultConfig returns the default handler configuration
func DefaultConfig() Config {
	return Config{
		DialTimeout:     10 * time.Second,
		IdleConnTimeout: 90 * time.Second,
	}
}

var transport *http.Transport

func init() {
	SetConfig(DefaultConfig())
}

// SetConfig updates the handler configuration
func SetConfig(c Config) {
	transport = &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: c.DialTimeout,
		}).DialContext,
		MaxIdleConns:        500,
		MaxIdleConnsPerHost: 200,
		IdleConnTimeout:     c.IdleConnTimeout,
	}
}

// HandleHTTP handles regular HTTP requests (non-CONNECT)
func HandleHTTP(w http.ResponseWriter, req *http.Request) {
	resp, err := transport.RoundTrip(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	defer resp.Body.Close()
	CopyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.CopyBuffer(w, resp.Body, make([]byte, 32*1024))
}

// CopyHeader copies HTTP headers from source to destination
func CopyHeader(dst, src http.Header) {
	hopHeaders := map[string]bool{
		"Connection":          true,
		"Proxy-Connection":    true,
		"Keep-Alive":          true,
		"Proxy-Authenticate":  true,
		"Proxy-Authorization": true,
		"Te":                  true,
		"Trailers":            true,
		"Transfer-Encoding":   true,
		"Upgrade":             true,
	}

	for k, vv := range src {
		if !hopHeaders[k] {
			for _, v := range vv {
				dst.Add(k, v)
			}
		}
	}
}

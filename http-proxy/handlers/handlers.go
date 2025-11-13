package handlers

import (
	"io"
	"net/http"
	"time"
)

var transport = &http.Transport{
	MaxIdleConns:        500,
	MaxIdleConnsPerHost: 200,
	IdleConnTimeout:     time.Second * 90,
}

// HandleHTTP handles regular HTTP requests (non-CONNECT)
func HandleHTTP(w http.ResponseWriter, req *http.Request) {
	resp, err := transport.RoundTrip(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	// log.Printf("Response Status: %d", resp.StatusCode)
	// log.Printf("Forwarded request from %s to %s", req.URL, resp.Request.URL)

	defer resp.Body.Close()
	CopyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.CopyBuffer(w, resp.Body, make([]byte, 32*1024))
	//io.Copy(io.Discard, resp.Body) // ensure body is fully read
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

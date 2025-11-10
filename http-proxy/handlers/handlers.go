package handlers

import (
	"io"
	"log"
	"net/http"
)

// HandleHTTP handles regular HTTP requests (non-CONNECT)
func HandleHTTP(w http.ResponseWriter, req *http.Request) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	log.Printf("Response Status: %d", resp.StatusCode)
	log.Printf("Forwarded request from %s to %s", req.URL, resp.Request.URL)

	defer resp.Body.Close()
	CopyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// CopyHeader copies HTTP headers from source to destination
func CopyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

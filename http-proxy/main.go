package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/aluko123/go-network-proxy/http-proxy/blocklist"
	"github.com/aluko123/go-network-proxy/http-proxy/handlers"
	"github.com/aluko123/go-network-proxy/http-proxy/metrics"
	"github.com/aluko123/go-network-proxy/http-proxy/tunnel"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type statusRecorder struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}

	r.statusCode = code
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(code)
}

func main() {
	// Parse command-line flags
	var pemPath string
	flag.StringVar(&pemPath, "pem", "server.pem", "path to pem file")
	var keyPath string
	flag.StringVar(&keyPath, "key", "server.key", "path to key file")
	var proto string
	flag.StringVar(&proto, "proto", "http", "protocol to use: http or https")
	var debug bool
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
	flag.Parse()

	// Initialize blocklist manager once at startup
	bm := blocklist.NewManager()
	if err := bm.LoadFromFile("blocklist/blocklist.json"); err != nil {
		log.Printf("Warning: Could not load blocklist: %v", err)
	}

	// Configure server
	server := &http.Server{
		Addr: ":8080",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Special case: metrics endpoint
			if r.URL.Path == "/metrics" {
				promhttp.Handler().ServeHTTP(w, r)
				return
			}

			//metrics timing start
			start := time.Now()
			metrics.ActiveConnections.Inc()
			defer metrics.ActiveConnections.Dec()

			//log only in debug mode
			if debug {
				//capture details of incoming request
				log.Printf("Method: %s, Host: %s, URL: %s\n", r.Method, r.Host, r.URL.String())
				for key, values := range r.Header {
					log.Printf("Request Header: %s: %v", key, values)
				}
			} else {
				log.Printf("[%s] %s", r.Method, r.Host)
			}

			host := r.Host
			if host == "" {
				host = r.URL.Host
			}

			//remove port if present
			if colonIdx := strings.Index(host, ":"); colonIdx != -1 {
				host = host[:colonIdx]
			}

			// Check blocklist BEFORE handling request (works for both HTTP and HTTPS)
			if bm.IsBlocked(host) {
				metrics.BlockedRequests.Inc()

				if r.Method == http.MethodConnect {
					http.Error(w, "Forbidden", http.StatusForbidden)

				} else {
					w.Header().Set("Content-Type", "text/html")
					w.WriteHeader(http.StatusForbidden)
					w.Write([]byte(blocklist.GetBlockedResponse()))
				}
				return
			}

			//var recorder statusRecorder
			var statusCode int = 200 // default to 200
			if r.Method == http.MethodConnect {
				tunnel.HandleTunneling(w, r)
			} else {
				recorder := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
				handlers.HandleHTTP(recorder, r)
				statusCode = recorder.statusCode
			}

			duration := time.Since(start).Seconds()
			metrics.RequestDuration.WithLabelValues(r.Method).Observe(duration)
			statusClass := fmt.Sprintf("%dxx", statusCode/100)
			metrics.StatusCodeCounter.WithLabelValues(statusClass).Inc()
			metrics.RequestsTotal.WithLabelValues(r.Method, http.StatusText(statusCode)).Inc()
		}),
		// Disable HTTP/2 to avoid issues with CONNECT method
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	// Start server
	log.Printf("Starting proxy server on %s (protocol: %s)", server.Addr, proto)
	if proto == "http" {
		log.Fatal(server.ListenAndServe())
	} else {
		log.Fatal(server.ListenAndServeTLS(pemPath, keyPath))
	}

}

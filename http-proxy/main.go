package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
)

func main() {
	// target, err := url.Parse("http://example.com")
	// if err != nil {
	// 	panic(err)
	// }

	//new ReverseProxy Instance
	//proxy := httputil.NewSingleHostReverseProxy(target)

	//proxy can use HTTPS
	// proxy.Transport = &http.Transport{
	// 	TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	// }

	//handler logs request and forwards to proxy
	handler := func(w http.ResponseWriter, r *http.Request) {
		//log.Println(r.URL)

		target := &url.URL{Scheme: r.URL.Scheme, Host: r.URL.Host}

		//if scheme is empty, default to http
		if target.Scheme == "" {
			target.Scheme = "http"
		}
		log.Printf("Proxying to: %s", target.String())
		proxy := httputil.NewSingleHostReverseProxy(target)
		r.Host = target.Host
		w.Header().Set("X-Proxy-By", "Go-Proxy")
		proxy.ServeHTTP(w, r)
	}

	http.HandleFunc("/", handler)
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		panic(err)
	}

}

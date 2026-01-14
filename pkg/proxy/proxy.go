package proxy

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// REMOVED: 'limiter' field from struct
type Relay struct {
	target *url.URL
	proxy  *httputil.ReverseProxy
}

func New(targetURL string) (*Relay, error) {
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}

	p := httputil.NewSingleHostReverseProxy(parsedURL)

	p.Director = func(req *http.Request) {
		req.URL.Scheme = parsedURL.Scheme
		req.URL.Host = parsedURL.Host
		req.Header.Set("X-Relay", "True")
	}

	// Log upstream errors so network/DNS/TLS issues are visible.
	p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("[PROXY] upstream error: %v", err)
		http.Error(w, "upstream error", http.StatusBadGateway)
	}

	// REMOVED: rate.NewLimiter code
	return &Relay{
		target: parsedURL,
		proxy:  p,
	}, nil
}

func (g *Relay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// REMOVED: limiter.Allow() check.
	// The Proxy is now "dumb" again (which is good!).
	g.proxy.ServeHTTP(w, r)
}

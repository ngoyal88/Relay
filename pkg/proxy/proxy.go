package proxy

import (
	"errors"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/sony/gobreaker"
)

// REMOVED: 'limiter' field from struct
type Relay struct {
	target *url.URL
	proxy  *httputil.ReverseProxy
	cb     *gobreaker.CircuitBreaker
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

	settings := gobreaker.Settings{
		Name:    "relay-upstream",
		Timeout: 30 * time.Second,
		ReadyToTrip: func(c gobreaker.Counts) bool {
			return c.ConsecutiveFailures >= 5
		},
	}

	return &Relay{
		target: parsedURL,
		proxy:  p,
		cb:     gobreaker.NewCircuitBreaker(settings),
	}, nil
}

func (g *Relay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if g.cb == nil {
		start := time.Now()
		g.proxy.ServeHTTP(w, r)
		upstreamLatency.Observe(time.Since(start).Seconds())
		return
	}

	if g.cb.State() == gobreaker.StateOpen {
		http.Error(w, "Service Unavailable (circuit open)", http.StatusServiceUnavailable)
		return
	}

	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	start := time.Now()

	_, err := g.cb.Execute(func() (interface{}, error) {
		g.proxy.ServeHTTP(rec, r)
		return nil, classifyStatus(rec.status)
	})

	upstreamLatency.Observe(time.Since(start).Seconds())

	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			http.Error(w, "Service Unavailable (circuit open)", http.StatusServiceUnavailable)
			return
		}
		// If the proxy already wrote a response, avoid double-writing.
		if rec.wrote {
			return
		}
		http.Error(w, "upstream error", http.StatusBadGateway)
	}
}

// classifyStatus returns an error for upstream 5xx to trip the breaker.
func classifyStatus(status int) error {
	if status >= 500 {
		return errors.New("upstream failure")
	}
	return nil
}

// statusRecorder captures status codes without changing response flow.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.wrote = true
	sr.ResponseWriter.WriteHeader(code)
}

func (sr *statusRecorder) Write(b []byte) (int, error) {
	if !sr.wrote {
		sr.status = http.StatusOK
		sr.wrote = true
	}
	return sr.ResponseWriter.Write(b)
}

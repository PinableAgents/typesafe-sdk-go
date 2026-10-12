package typesafe

import (
	"net"
	"net/http"
	"time"
)

// NewPooledTransport returns an independent, opt-in transport for bursty Agent
// workloads. Pass it as Config.Transport; configure proxy/TLS before first use.
// It keeps at most 100 idle connections (also per host) for 30 seconds and caps
// host connections at 100. HTTP/2 streams are NOT limited to 100 requests by this
// connection cap: bound in-flight evaluations separately in the host.
// It never mutates http.DefaultTransport or disables certificate verification.
// Existing NewClient defaults and borrowed HTTPClient ownership are unchanged.
func NewPooledTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
		MaxConnsPerHost:       100,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

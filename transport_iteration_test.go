package typesafe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPooledTransportIsolation(t *testing.T) {
	before := http.DefaultTransport.(*http.Transport).MaxIdleConnsPerHost
	a, b := NewPooledTransport(), NewPooledTransport()
	defer a.CloseIdleConnections()
	defer b.CloseIdleConnections()
	if a == b || a.MaxIdleConnsPerHost != 100 || a.MaxConnsPerHost != 100 || a.IdleConnTimeout != 30*time.Second || !a.ForceAttemptHTTP2 {
		t.Fatal("profile")
	}
	a.MaxIdleConnsPerHost = 3
	if b.MaxIdleConnsPerHost != 100 || http.DefaultTransport.(*http.Transport).MaxIdleConnsPerHost != before {
		t.Fatal("shared configuration")
	}
}

func TestBurstConnectionReuse(t *testing.T) {
	for _, pooled := range []bool{false, true} {
		t.Run(fmt.Sprint(pooled), func(t *testing.T) {
			const n = 20
			var calls, connections atomic.Int32
			gates := []chan struct{}{make(chan struct{}), make(chan struct{})}
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				index := int(calls.Add(1)) - 1
				batch := index / n
				if index%n == n-1 {
					close(gates[batch])
				}
				select {
				case <-gates[batch]:
				case <-r.Context().Done():
					return
				}
				fmt.Fprint(w, `{"models":[]}`)
			}))
			srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					connections.Add(1)
				}
			}
			srv.Start()
			defer srv.Close()
			tr := http.DefaultTransport.(*http.Transport).Clone()
			if pooled {
				tr = NewPooledTransport()
			}
			tr.Proxy = nil
			c, err := NewClient(Config{APIKey: "fixture", Transport: tr, BaseURL: srv.URL, AllowInsecureHTTP: true, Retry: &RetryPolicy{}})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			first := int32(0)
			for batch := 0; batch < 2; batch++ {
				results := make(chan error, n)
				for i := 0; i < n; i++ {
					go func() { _, err := c.ListModels(ctx); results <- err }()
				}
				for i := 0; i < n; i++ {
					if err := <-results; err != nil {
						t.Fatal(err)
					}
				}
				if batch == 0 {
					first = connections.Load()
				}
			}
			second := connections.Load() - first
			t.Logf("pooled=%t first_new_connections=%d second_new_connections=%d", pooled, first, second)
			if first != n || (pooled && second != 0) || (!pooled && second < n-2) {
				t.Fatal("unexpected connection reuse")
			}
		})
	}
}

func TestPooledHTTP2AndCancellation(t *testing.T) {
	var connections atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Block") == "yes" {
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, `{"models":[]}`)
	}))
	srv.EnableHTTP2 = true
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	srv.StartTLS()
	defer srv.Close()
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	tr := NewPooledTransport()
	tr.Proxy = nil
	tr.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	c, err := NewClient(Config{APIKey: "fixture", BaseURL: srv.URL, Transport: tr, Retry: &RetryPolicy{}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	warm, err := c.ListModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if warm.HTTP.Protocol != "HTTP/2.0" {
		t.Fatalf("protocol %q", warm.HTTP.Protocol)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := c.ListModels(ctx)
			if err != nil || r.HTTP.Protocol != "HTTP/2.0" {
				t.Errorf("HTTP2: %v", err)
			}
		}()
	}
	wg.Wait()
	if connections.Load() != 1 {
		t.Fatalf("HTTP2 warm connection not reused: %d", connections.Load())
	}
	blocked, done := context.WithTimeout(ctx, 30*time.Millisecond)
	defer done()
	_, err = c.ListModels(blocked, WithHeaders(http.Header{"X-Test-Block": []string{"yes"}}))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel: %v", err)
	}
	if _, err = c.ListModels(ctx); err != nil {
		t.Fatal("cancel poisoned shared connection", err)
	}
}

func TestPooledConnectionQueueDeadline(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, `{"models":[]}`)
	}))
	defer srv.Close()
	defer close(release)
	tr := NewPooledTransport()
	tr.Proxy = nil
	tr.MaxConnsPerHost = 1
	c, err := NewClient(Config{APIKey: "fixture", BaseURL: srv.URL, AllowInsecureHTTP: true, Transport: tr, Retry: &RetryPolicy{}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	first, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, err := c.ListModels(first); finished <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first call did not start")
	}
	ctx, done := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer done()
	_, err = c.ListModels(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("queue ignored deadline", err)
	}
	cancel()
	<-finished
}

func BenchmarkPooledTransportRequests(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"models":[]}`) }))
	defer srv.Close()
	c, err := NewClient(Config{APIKey: "fixture", BaseURL: srv.URL, AllowInsecureHTTP: true, Transport: NewPooledTransport(), Retry: &RetryPolicy{}})
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := c.ListModels(context.Background()); err != nil {
				b.Error(err)
			}
		}
	})
}

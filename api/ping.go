package handler

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"
)

const (
	Count       = 10
	Concurrency = 8
	Timeout     = 5 * time.Second
)

type pingResult struct {
	key string
	d   time.Duration
	err error
}

func Ping(w http.ResponseWriter, r *http.Request) {
	ep := []string{
		"gateway01.us-east-1.dev.shared.aws.tidbcloud.com",
		"acc-gateway01.us-east-1.dev.shared.aws.tidbcloud.com",
	}
	results := pingEndpoints(context.Background(), ep)

	fmt.Fprintf(w, printlnPingResult(results))
}

func printlnPingResult(results []pingResult) string {
	var result string
	for _, r := range results {
		if r.err == nil {
			result += fmt.Sprintf("%s %s %s\n", r.key, r.d, "nil")
		} else {
			result += fmt.Sprintf("%s %s %s\n", r.key, r.d, r.err)
		}
	}
	return result
}

func pingEndpoints(ctx context.Context, eps []string) []pingResult {
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		sem     = make(chan struct{}, Concurrency)
		results = make([]pingResult, 0, len(eps))
	)

	defer close(sem)

	for _, ep := range eps {
		ep := ep
		wg.Add(1)
		select {
		case <-ctx.Done():
			return results
		case sem <- struct{}{}:
			go func() {
				var sum time.Duration
				var resultErr error
				for i := 0; i < Count; i++ {
					d, err := pingEndpoint(ctx, ep, Timeout)
					// XXX: on failures, set the duration to the timeout so they are sorted last
					if err != nil {
						resultErr = err
						d = Timeout
					}
					sum += d
				}

				mu.Lock()
				results = append(results, pingResult{
					key: ep,
					d:   sum / time.Duration(Count),
					err: resultErr,
				})
				mu.Unlock()
				wg.Done()
				<-sem
			}()
		}
	}

	wg.Wait()
	return results
}

func pingEndpoint(ctx context.Context, hostname string, timeout time.Duration) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// make hostname a FQDN if not already
	if hostname[len(hostname)-1] != '.' {
		hostname = hostname + "."
	}

	// separately look up DNS so that's separate from our connection time
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", hostname)
	if err != nil {
		return 0, err
	}
	if len(addrs) == 0 {
		return 0, fmt.Errorf("unable to resolve addr: %s", hostname)
	}

	// explicitly time to establish a TCP connection
	var d net.Dialer
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", netip.AddrPortFrom(addrs[0], 4000).String())
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	return time.Since(start), nil
}

package tsproxy

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestHttpsRedirect(t *testing.T) {
	tests := []struct {
		name       string
		targetPort int
		wantHost   string // expected Location host (without scheme)
	}{
		{name: "default 443", targetPort: 443, wantHost: ""},       // no port appended for default HTTPS
		{name: "custom port", targetPort: 8443, wantHost: ":8443"}, // target port appended
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			listenPort := freePort(t)
			redirect := NewHttpsRedirect("https_redirect", listenPort, tc.targetPort)
			t.Cleanup(redirect.Close)

			before := metric(t, connectionsCount.WithLabelValues("https_redirect", itoa(listenPort), ""))

			client := &http.Client{
				CheckRedirect: func(*http.Request, []*http.Request) error {
					return http.ErrUseLastResponse
				},
				Timeout: 2 * time.Second,
			}

			url := fmt.Sprintf("http://127.0.0.1:%d/foo?bar=1", listenPort)
			resp := getWithRetry(t, client, url)
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusMovedPermanently {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusMovedPermanently)
			}

			host := "127.0.0.1"
			want := fmt.Sprintf("https://%s%s/foo?bar=1", host, tc.wantHost)
			if loc := resp.Header.Get("Location"); loc != want {
				t.Errorf("Location = %q, want %q", loc, want)
			}

			if c := metric(t, connectionsCount.WithLabelValues("https_redirect", itoa(listenPort), "")); c != before+1 {
				t.Errorf("connectionsCount = %v, want %v", c, before+1)
			}
		})
	}
}

func getWithRetry(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	var resp *http.Response
	var err error
	for range 100 {
		resp, err = client.Get(url)
		if err == nil {
			return resp
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("GET %s: %v", url, err)
	return nil
}

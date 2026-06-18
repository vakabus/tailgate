package tsproxy

import (
	"testing"

	"github.com/coredns/caddy"
	"github.com/google/go-cmp/cmp"
)

func TestParseChannels(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		shouldErr bool
		want      []channel
	}{
		{
			name:  "tcp",
			input: "tsproxy {\n tcp 10080 -> vrejsek 80\n}",
			want:  []channel{{protocol: "tcp", myPort: 10080, target: "vrejsek", targetPort: 80}},
		},
		{
			name:  "tcp_proxy",
			input: "tsproxy {\n tcp_proxy 10443 -> vrejsek 443\n}",
			want:  []channel{{protocol: "tcp_proxy", myPort: 10443, target: "vrejsek", targetPort: 443}},
		},
		{
			name:  "udp",
			input: "tsproxy {\n udp 10053 -> vrejsek 53\n}",
			want:  []channel{{protocol: "udp", myPort: 10053, target: "vrejsek", targetPort: 53}},
		},
		{
			name:  "https_redirect",
			input: "tsproxy {\n https_redirect 10080 -> 443\n}",
			want:  []channel{{protocol: "https_redirect", myPort: 10080, targetPort: 443}},
		},
		{
			name: "multiple channels",
			input: `tsproxy {
				tcp 10080 -> vrejsek 80
				udp 10053 -> vrejsek 53
				https_redirect 10081 -> 8443
			}`,
			want: []channel{
				{protocol: "tcp", myPort: 10080, target: "vrejsek", targetPort: 80},
				{protocol: "udp", myPort: 10053, target: "vrejsek", targetPort: 53},
				{protocol: "https_redirect", myPort: 10081, targetPort: 8443},
			},
		},
		// Error cases.
		{
			name:      "short args must not panic",
			input:     "tsproxy {\n tcp 80\n}",
			shouldErr: true,
		},
		{
			name:      "missing arrow",
			input:     "tsproxy {\n tcp 10080 vrejsek 80\n}",
			shouldErr: true,
		},
		{
			name:      "non-numeric listen port",
			input:     "tsproxy {\n tcp notaport -> vrejsek 80\n}",
			shouldErr: true,
		},
		{
			name:      "out-of-range port",
			input:     "tsproxy {\n tcp 99999 -> vrejsek 80\n}",
			shouldErr: true,
		},
		{
			name:      "https_redirect wrong arity",
			input:     "tsproxy {\n https_redirect 10080 -> vrejsek 443\n}",
			shouldErr: true,
		},
		{
			name:      "unknown token",
			input:     "tsproxy {\n sctp 10080 -> vrejsek 80\n}",
			shouldErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := caddy.NewTestController("dns", tc.input)
			got, err := parseChannels(c)
			if tc.shouldErr {
				if err == nil {
					t.Fatalf("expected error, got channels %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(channel{})); diff != "" {
				t.Errorf("channels mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

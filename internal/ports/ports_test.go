package ports

import (
	"strings"
	"testing"
)

func TestParsePortListSingleSpecForms(t *testing.T) {
	tests := []struct {
		name string
		spec string
		want PortMapping
	}{
		{
			name: "container only",
			spec: "3000",
			want: PortMapping{HostIP: "127.0.0.1", HostPort: "3000", ContainerPort: "3000", Protocol: "tcp"},
		},
		{
			name: "host and container",
			spec: "8080:80",
			want: PortMapping{HostIP: "127.0.0.1", HostPort: "8080", ContainerPort: "80", Protocol: "tcp"},
		},
		{
			name: "host ip host and container",
			spec: "0.0.0.0:8080:80",
			want: PortMapping{HostIP: "0.0.0.0", HostPort: "8080", ContainerPort: "80", Protocol: "tcp"},
		},
		{
			name: "udp",
			spec: "3000/udp",
			want: PortMapping{HostIP: "127.0.0.1", HostPort: "3000", ContainerPort: "3000", Protocol: "udp"},
		},
		{
			name: "dynamic host port",
			spec: "0:3000",
			want: PortMapping{HostIP: "127.0.0.1", HostPort: "0", ContainerPort: "3000", Protocol: "tcp"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePortList([]string{tt.spec})
			if err != nil {
				t.Fatalf("ParsePortList: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("got %d ports, want 1: %#v", len(got), got)
			}
			if got[0] != tt.want {
				t.Fatalf("port mismatch:\n got: %#v\nwant: %#v", got[0], tt.want)
			}
		})
	}
}

func TestParsePortListCommaSplitting(t *testing.T) {
	got, err := ParsePortList([]string{"3000,8080:80", "0:9000/udp"})
	if err != nil {
		t.Fatalf("ParsePortList: %v", err)
	}
	want := []PortMapping{
		{HostIP: "127.0.0.1", HostPort: "3000", ContainerPort: "3000", Protocol: "tcp"},
		{HostIP: "127.0.0.1", HostPort: "8080", ContainerPort: "80", Protocol: "tcp"},
		{HostIP: "127.0.0.1", HostPort: "0", ContainerPort: "9000", Protocol: "udp"},
	}
	if !samePortOrder(got, want) {
		t.Fatalf("ports mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestParsePortChangeSigilSemantics(t *testing.T) {
	current := []PortMapping{
		{HostIP: "127.0.0.1", HostPort: "3001", ContainerPort: "3001", Protocol: "tcp"},
		{HostIP: "127.0.0.1", HostPort: "3003", ContainerPort: "3003", Protocol: "tcp"},
	}

	t.Run("additive and subtractive", func(t *testing.T) {
		got, err := ParsePortChange([]string{"3000,-3001,+3002"}, current)
		if err != nil {
			t.Fatalf("ParsePortChange: %v", err)
		}
		want := []PortMapping{
			{HostIP: "127.0.0.1", HostPort: "3003", ContainerPort: "3003", Protocol: "tcp"},
			{HostIP: "127.0.0.1", HostPort: "3000", ContainerPort: "3000", Protocol: "tcp"},
			{HostIP: "127.0.0.1", HostPort: "3002", ContainerPort: "3002", Protocol: "tcp"},
		}
		if !PortMappingsEqual(got, want) {
			t.Fatalf("ports mismatch:\n got: %#v\nwant: %#v", got, want)
		}
	})

	t.Run("replace", func(t *testing.T) {
		got, err := ParsePortChange([]string{"=3000,+8080:80"}, current)
		if err != nil {
			t.Fatalf("ParsePortChange: %v", err)
		}
		want := []PortMapping{
			{HostIP: "127.0.0.1", HostPort: "3000", ContainerPort: "3000", Protocol: "tcp"},
			{HostIP: "127.0.0.1", HostPort: "8080", ContainerPort: "80", Protocol: "tcp"},
		}
		if !PortMappingsEqual(got, want) {
			t.Fatalf("ports mismatch:\n got: %#v\nwant: %#v", got, want)
		}
	})

	t.Run("duplicate add is no-op", func(t *testing.T) {
		got, err := ParsePortChange([]string{"3000,+3000,0.0.0.0:3000:3000"}, nil)
		if err != nil {
			t.Fatalf("ParsePortChange: %v", err)
		}
		want := []PortMapping{
			{HostIP: "127.0.0.1", HostPort: "3000", ContainerPort: "3000", Protocol: "tcp"},
		}
		if !samePortOrder(got, want) {
			t.Fatalf("ports mismatch:\n got: %#v\nwant: %#v", got, want)
		}
	})
}

func TestParsePortChangeErrors(t *testing.T) {
	current := []PortMapping{
		{HostIP: "127.0.0.1", HostPort: "3000", ContainerPort: "3000", Protocol: "tcp"},
	}
	tests := []struct {
		name string
		spec []string
		want string
	}{
		{name: "bad port", spec: []string{"70000"}, want: "container port \"70000\" must be between 1 and 65535"},
		{name: "bad ip", spec: []string{"not-an-ip:8080:80"}, want: "invalid host IP \"not-an-ip\""},
		{name: "bad protocol", spec: []string{"3000/sctp"}, want: "invalid port protocol \"sctp\""},
		{name: "range", spec: []string{"3000-3010"}, want: portRangeErrorText},
		{name: "replace remove", spec: []string{"=3000,-3001"}, want: "cannot use - prefix in replace mode (=)"},
		{name: "missing remove", spec: []string{"-3001"}, want: "port 3001/tcp is not currently mapped"},
		{name: "add remove conflict", spec: []string{"3000,-3000"}, want: "port 3000 cannot be both added and removed in one command"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePortChange(tt.spec, current)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error mismatch:\n got: %v\nwant substring: %q", err, tt.want)
			}
		})
	}
}

func TestPortMappingsEqual(t *testing.T) {
	a := []PortMapping{
		{HostIP: "127.0.0.1", HostPort: "3000", ContainerPort: "3000", Protocol: "tcp"},
		{HostIP: "0.0.0.0", HostPort: "8080", ContainerPort: "80", Protocol: "udp"},
	}
	b := []PortMapping{
		{HostIP: "0.0.0.0", HostPort: "8080", ContainerPort: "80", Protocol: "udp"},
		{HostIP: "127.0.0.1", HostPort: "3000", ContainerPort: "3000", Protocol: "tcp"},
	}
	if !PortMappingsEqual(a, b) {
		t.Fatalf("same mappings in different order should match")
	}
	if PortMappingsEqual(a, []PortMapping{
		{HostIP: "127.0.0.1", HostPort: "3000", ContainerPort: "3000", Protocol: "tcp"},
		{HostIP: "127.0.0.1", HostPort: "8080", ContainerPort: "80", Protocol: "udp"},
	}) {
		t.Fatalf("different host IP should not match")
	}
	if PortMappingsEqual(a, []PortMapping{
		{HostIP: "127.0.0.1", HostPort: "3000", ContainerPort: "3000", Protocol: "tcp"},
		{HostIP: "0.0.0.0", HostPort: "8080", ContainerPort: "80", Protocol: "tcp"},
	}) {
		t.Fatalf("different protocol should not match")
	}
}

func TestFormatPortMappingAndList(t *testing.T) {
	tests := []struct {
		name string
		port PortMapping
		want string
	}{
		{name: "same host container tcp", port: PortMapping{HostIP: "127.0.0.1", HostPort: "3000", ContainerPort: "3000", Protocol: "tcp"}, want: "3000"},
		{name: "different host container tcp", port: PortMapping{HostIP: "127.0.0.1", HostPort: "8080", ContainerPort: "80", Protocol: "tcp"}, want: "8080:80"},
		{name: "non default host ip", port: PortMapping{HostIP: "0.0.0.0", HostPort: "8080", ContainerPort: "80", Protocol: "tcp"}, want: "0.0.0.0:8080:80"},
		{name: "udp", port: PortMapping{HostIP: "127.0.0.1", HostPort: "3000", ContainerPort: "3000", Protocol: "udp"}, want: "3000/udp"},
		{name: "dynamic", port: PortMapping{HostIP: "127.0.0.1", HostPort: "0", ContainerPort: "3000", Protocol: "tcp"}, want: "0:3000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatPortMapping(tt.port); got != tt.want {
				t.Fatalf("FormatPortMapping = %q, want %q", got, tt.want)
			}
		})
	}

	ports := []PortMapping{
		{HostIP: "127.0.0.1", HostPort: "8080", ContainerPort: "80", Protocol: "tcp"},
		{HostIP: "127.0.0.1", HostPort: "3000", ContainerPort: "3000", Protocol: "tcp"},
		{HostIP: "127.0.0.1", HostPort: "3001", ContainerPort: "3001", Protocol: "udp"},
	}
	if got, want := FormatPortList(ports), "3000, 3001/udp, 8080:80"; got != want {
		t.Fatalf("FormatPortList = %q, want %q", got, want)
	}
}

func samePortOrder(a, b []PortMapping) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

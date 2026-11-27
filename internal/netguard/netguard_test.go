package netguard

import (
	"net/netip"
	"testing"
)

func TestAllowedRemoteAddr(t *testing.T) {
	ok := []string{"127.0.0.1:1", "[::1]:1", "192.168.1.20:5", "10.0.0.3:5", "172.20.1.1:5",
		"100.101.102.103:5", "[fd7a:115c:a1e0::1]:5", "[fe80::1%eth0]:5", "[::ffff:192.168.1.2]:5"}
	bad := []string{"8.8.8.8:1", "[2001:4860::8888]:1", "100.128.0.1:5", "172.32.0.1:5", "basura", "[::ffff:8.8.8.8]:1"}
	for _, a := range ok {
		if !AllowedRemoteAddr(a) {
			t.Errorf("%s debería poder entrar", a)
		}
	}
	for _, a := range bad {
		if AllowedRemoteAddr(a) {
			t.Errorf("%s NO debería poder entrar", a)
		}
	}
}

func TestIsPrivatePrefix(t *testing.T) {
	if !IsPrivatePrefix(netip.MustParsePrefix("192.168.50.0/24")) {
		t.Fatal("red privada rechazada")
	}
	if IsPrivatePrefix(netip.MustParsePrefix("8.8.8.0/24")) {
		t.Fatal("red pública aceptada")
	}
}

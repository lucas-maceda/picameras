// Package netguard decide desde qué direcciones se acepta una conexión.
//
// PiCameras solo atiende a la red local y a redes privadas superpuestas (Tailscale, WireGuard). Una conexión
// desde una IP pública se rechaza siempre, aunque alguien abra un puerto en el router por error.
package netguard

import (
	"net"
	"net/netip"
)

// cgnat es 100.64.0.0/10: el rango que usa Tailscale para sus IPs.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// Allowed indica si una IP puede hablar con PiCameras.
func Allowed(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || cgnat.Contains(ip)
}

// AllowedRemoteAddr hace lo mismo con un RemoteAddr de net/http ("ip:puerto").
func AllowedRemoteAddr(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return Allowed(ip.WithZone(""))
}

// IsPrivatePrefix indica si un rango entero es privado (para escanear otra red, por ejemplo la del pueblo).
func IsPrivatePrefix(p netip.Prefix) bool {
	p = p.Masked()
	first := p.Addr()
	if !first.Is4() {
		return false
	}
	return first.IsPrivate() || cgnat.Contains(first)
}

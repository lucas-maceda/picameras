// Package discover busca cámaras en la red: WS-Discovery (ONVIF) por multicast y un barrido de puertos RTSP y
// HTTP. El barrido también vale para redes remotas unidas por Tailscale o WireGuard, donde el multicast no llega.
package discover

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lucas-maceda/picameras/internal/auth"
	"github.com/lucas-maceda/picameras/internal/camera"
	"github.com/lucas-maceda/picameras/internal/netguard"
)

// Puertos que se miran en cada equipo.
var (
	rtspPorts = []int{554, 8554}
	httpPorts = []int{80, 8080}
)

// Found es una cámara candidata.
type Found struct {
	IP       string     `json:"ip"`
	Ports    []int      `json:"ports"`
	Name     string     `json:"name,omitempty"`
	Hardware string     `json:"hardware,omitempty"`
	Vendor   string     `json:"vendor,omitempty"`
	ONVIF    string     `json:"onvif,omitempty"`
	Suggest  Suggestion `json:"suggest"`
}

// Suggestion es lo que se propone en el formulario de alta.
type Suggestion struct {
	Kind string `json:"kind"`
	Port int    `json:"port,omitempty"`
}

// Options controla una búsqueda.
type Options struct {
	// Prefix, si no está vacío, escanea solo ese rango (por ejemplo la red del pueblo, 192.168.50.0/24).
	Prefix string
	// Timeout por conexión.
	DialTimeout time.Duration
}

// ParsePrefix valida un rango para escanear: IPv4 privada y como mucho 1024 direcciones.
func ParsePrefix(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "/") {
		s += "/24"
	}
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return p, errors.New("rango no válido; escribe algo como 192.168.50.0/24")
	}
	p = p.Masked()
	if !netguard.IsPrivatePrefix(p) {
		return p, errors.New("solo se pueden escanear redes privadas (192.168.x.x, 10.x.x.x, 172.16-31.x.x o Tailscale)")
	}
	if p.Bits() < 22 {
		return p, errors.New("rango demasiado grande: como mucho /22 (1024 direcciones)")
	}
	return p, nil
}

// LocalPrefixes devuelve las redes locales del equipo, recortadas a /24 alrededor de nuestra IP.
func LocalPrefixes() []netip.Prefix {
	var out []netip.Prefix
	ifs, _ := net.Interfaces()
	for _, itf := range ifs {
		if itf.Flags&net.FlagUp == 0 || itf.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := itf.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip, ok := netip.AddrFromSlice(n.IP.To4())
			if !ok || !ip.IsPrivate() {
				continue // ni IPv6, ni Tailscale (100.64/10), ni IPs públicas
			}
			bits, _ := n.Mask.Size()
			if bits < 24 {
				bits = 24
			}
			p, _ := ip.Prefix(bits)
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// Run hace la búsqueda.
func Run(ctx context.Context, opt Options) ([]Found, error) {
	if opt.DialTimeout == 0 {
		opt.DialTimeout = 700 * time.Millisecond
	}
	var prefixes []netip.Prefix
	remote := opt.Prefix != ""
	if remote {
		p, err := ParsePrefix(opt.Prefix)
		if err != nil {
			return nil, err
		}
		prefixes = []netip.Prefix{p}
	} else {
		prefixes = LocalPrefixes()
	}

	res := newResults()
	var wg sync.WaitGroup
	if !remote {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, d := range wsDiscovery(ctx, 3*time.Second) {
				res.onvif(d)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		scan(ctx, prefixes, opt.DialTimeout, res)
	}()
	wg.Wait()

	fingerprint(ctx, res)
	return res.list(), nil
}

// ---- resultados ----

type results struct {
	mu sync.Mutex
	m  map[string]*Found
}

func newResults() *results { return &results{m: map[string]*Found{}} }

func (r *results) get(ip string) *Found {
	f, ok := r.m[ip]
	if !ok {
		f = &Found{IP: ip}
		r.m[ip] = f
	}
	return f
}

func (r *results) port(ip string, port int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f := r.get(ip)
	if !slices.Contains(f.Ports, port) {
		f.Ports = append(f.Ports, port)
		sort.Ints(f.Ports)
	}
}

func (r *results) onvif(d onvifDevice) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f := r.get(d.IP)
	f.ONVIF = d.XAddr
	f.Name = d.Name
	f.Hardware = d.Hardware
}

func (r *results) list() []Found {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Found
	for _, f := range r.m {
		hasRTSP := slices.ContainsFunc(f.Ports, func(p int) bool { return slices.Contains(rtspPorts, p) })
		if f.ONVIF == "" && !hasRTSP && f.Suggest.Kind != camera.KindYiHack {
			continue // solo puertos web: routers, impresoras… no son cámaras
		}
		if f.Ports == nil {
			f.Ports = []int{} // en JSON, [] y no null
		}
		if f.Suggest.Kind == "" {
			switch {
			case f.ONVIF != "":
				f.Suggest = Suggestion{Kind: camera.KindONVIF, Port: onvifPort(f.ONVIF)}
			default:
				port := 554
				if !slices.Contains(f.Ports, 554) {
					port = 8554
				}
				f.Suggest = Suggestion{Kind: camera.KindRTSP, Port: port}
			}
		}
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := netip.ParseAddr(out[i].IP)
		b, _ := netip.ParseAddr(out[j].IP)
		return a.Less(b)
	})
	return out
}

func onvifPort(xaddr string) int {
	u, err := url.Parse(xaddr)
	if err != nil {
		return 0
	}
	p, _ := strconv.Atoi(u.Port())
	return p
}

// ---- barrido de puertos ----

func scan(ctx context.Context, prefixes []netip.Prefix, timeout time.Duration, res *results) {
	self := map[netip.Addr]bool{}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				if ip, ok := netip.AddrFromSlice(n.IP.To4()); ok {
					self[ip] = true
				}
			}
		}
	}
	type job struct {
		ip   netip.Addr
		port int
	}
	jobs := make(chan job)
	var wg sync.WaitGroup
	for i := 0; i < 128; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d := net.Dialer{Timeout: timeout}
			for j := range jobs {
				addr := net.JoinHostPort(j.ip.String(), strconv.Itoa(j.port))
				c, err := d.DialContext(ctx, "tcp", addr)
				if err == nil {
					c.Close()
					res.port(j.ip.String(), j.port)
				}
			}
		}()
	}
	ports := append(append([]int{}, rtspPorts...), httpPorts...)
loop:
	for _, p := range prefixes {
		for ip := p.Masked().Addr(); p.Contains(ip); ip = ip.Next() {
			b := ip.As4()
			if b[3] == 0 || b[3] == 255 || self[ip] {
				continue
			}
			for _, port := range ports {
				select {
				case jobs <- job{ip, port}:
				case <-ctx.Done():
					break loop
				}
			}
		}
	}
	close(jobs)
	wg.Wait()
}

// ---- huella por HTTP ----

var titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

func fingerprint(ctx context.Context, res *results) {
	res.mu.Lock()
	var targets []*Found
	for _, f := range res.m {
		if slices.ContainsFunc(f.Ports, func(p int) bool { return slices.Contains(httpPorts, p) }) {
			targets = append(targets, f)
		}
	}
	res.mu.Unlock()

	client := &http.Client{
		Timeout: 2500 * time.Millisecond,
		Transport: &http.Transport{
			// Solo para leer el título de la página de la cámara en la red local; no se envía ningún secreto.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
			Proxy:           nil,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for _, f := range targets {
		wg.Add(1)
		go func(f *Found) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			port := 80
			res.mu.Lock()
			if !slices.Contains(f.Ports, 80) {
				port = 8080
			}
			ip := f.IP
			res.mu.Unlock()
			vendor, yi := probeHTTP(ctx, client, fmt.Sprintf("http://%s/", net.JoinHostPort(ip, strconv.Itoa(port))))
			res.mu.Lock()
			if vendor != "" && f.Vendor == "" {
				f.Vendor = vendor
			}
			if yi {
				f.Vendor = "YI (yi-hack)"
				f.Suggest = Suggestion{Kind: camera.KindYiHack}
			}
			res.mu.Unlock()
		}(f)
	}
	wg.Wait()
}

func probeHTTP(ctx context.Context, c *http.Client, u string) (vendor string, yiHack bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("User-Agent", "PiCameras-discovery")
	resp, err := c.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	text := strings.ToLower(string(body))
	server := strings.ToLower(resp.Header.Get("Server"))
	if strings.Contains(text, "yi-hack") {
		return "YI (yi-hack)", true
	}
	known := []struct{ needle, name string }{
		{"hikvision", "Hikvision"}, {"app-webs", "Hikvision"}, {"dahua", "Dahua"}, {"dh_web", "Dahua"},
		{"reolink", "Reolink"}, {"axis", "Axis"}, {"foscam", "Foscam"}, {"amcrest", "Amcrest"},
		{"uniview", "Uniview"}, {"tp-link", "TP-Link"}, {"ezviz", "Ezviz"}, {"hipcam", "Hipcam"},
		{"ipcam", "Cámara IP"}, {"dvr", "DVR"}, {"nvr", "NVR"},
	}
	for _, k := range known {
		if strings.Contains(server, k.needle) || strings.Contains(text, k.needle) {
			return k.name, false
		}
	}
	if m := titleRe.FindStringSubmatch(string(body)); m != nil {
		t := strings.TrimSpace(m[1])
		if len(t) > 40 {
			t = t[:40]
		}
		return t, false
	}
	return "", false
}

// ---- WS-Discovery ----

type onvifDevice struct {
	IP, XAddr, Name, Hardware string
}

const probeTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<e:Envelope xmlns:e="http://www.w3.org/2003/05/soap-envelope" xmlns:w="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery" xmlns:dn="http://www.onvif.org/ver10/network/wsdl">
<e:Header><w:MessageID>uuid:%s</w:MessageID><w:To e:mustUnderstand="true">urn:schemas-xmlsoap-org:ws:2005:04:discovery</w:To><w:Action e:mustUnderstand="true">http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</w:Action></e:Header>
<e:Body><d:Probe><d:Types>dn:NetworkVideoTransmitter</d:Types></d:Probe></e:Body></e:Envelope>`

var (
	xaddrsRe = regexp.MustCompile(`(?s)<[^>]*XAddrs>\s*([^<]+?)\s*<`)
	scopesRe = regexp.MustCompile(`(?s)<[^>]*Scopes[^>]*>\s*([^<]+?)\s*<`)
)

func uuid() string {
	t := auth.RandomID() + auth.RandomID()
	return t[0:8] + "-" + t[8:12] + "-4" + t[13:16] + "-a" + t[17:20] + "-" + t[20:32]
}

func wsDiscovery(ctx context.Context, wait time.Duration) []onvifDevice {
	dst := &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 3702}
	var mu sync.Mutex
	found := map[string]onvifDevice{}
	var wg sync.WaitGroup
	ifs, _ := net.Interfaces()
	for _, itf := range ifs {
		if itf.Flags&net.FlagUp == 0 || itf.Flags&net.FlagMulticast == 0 || itf.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := itf.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil || !n.IP.IsPrivate() {
				continue
			}
			wg.Add(1)
			go func(local net.IP) {
				defer wg.Done()
				conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: local})
				if err != nil {
					return
				}
				defer conn.Close()
				msg := []byte(fmt.Sprintf(probeTmpl, uuid()))
				for i := 0; i < 2; i++ {
					_, _ = conn.WriteToUDP(msg, dst)
				}
				deadline := time.Now().Add(wait)
				if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
					deadline = d
				}
				_ = conn.SetReadDeadline(deadline)
				buf := make([]byte, 64*1024)
				for {
					n, from, err := conn.ReadFromUDP(buf)
					if err != nil {
						return
					}
					if d, ok := parseProbeMatch(buf[:n], from.IP.String()); ok {
						mu.Lock()
						found[d.IP] = d
						mu.Unlock()
					}
				}
			}(n.IP.To4())
		}
	}
	wg.Wait()
	var out []onvifDevice
	for _, d := range found {
		out = append(out, d)
	}
	return out
}

func parseProbeMatch(b []byte, fromIP string) (onvifDevice, bool) {
	s := string(b)
	if !strings.Contains(s, "ProbeMatch") {
		return onvifDevice{}, false
	}
	d := onvifDevice{IP: fromIP}
	if m := xaddrsRe.FindStringSubmatch(s); m != nil {
		for _, x := range strings.Fields(m[1]) {
			u, err := url.Parse(x)
			if err != nil {
				continue
			}
			if d.XAddr == "" || u.Hostname() == fromIP {
				d.XAddr = x
			}
		}
	}
	if m := scopesRe.FindStringSubmatch(s); m != nil {
		for _, sc := range strings.Fields(m[1]) {
			for _, k := range []struct {
				prefix string
				dst    *string
			}{{"onvif://www.onvif.org/name/", &d.Name}, {"onvif://www.onvif.org/hardware/", &d.Hardware}} {
				if strings.HasPrefix(sc, k.prefix) {
					v, err := url.PathUnescape(strings.TrimPrefix(sc, k.prefix))
					if err == nil && len(v) <= 64 {
						*k.dst = v
					}
				}
			}
		}
	}
	return d, d.XAddr != ""
}

package discover

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParsePrefix(t *testing.T) {
	ok := map[string]string{"192.168.99.0/24": "192.168.99.0/24", "192.168.99.7": "192.168.99.0/24",
		"10.99.0.0/22": "10.99.0.0/22", "100.100.100.0/24": "100.100.100.0/24"}
	for in, want := range ok {
		p, err := ParsePrefix(in)
		if err != nil || p.String() != want {
			t.Errorf("%s: %v %v", in, p, err)
		}
	}
	for _, in := range []string{"203.0.113.0/24", "10.0.0.0/16", "basura", "fd00::/64"} {
		if _, err := ParsePrefix(in); err == nil {
			t.Errorf("%s aceptado", in)
		}
	}
}

func TestParseProbeMatch(t *testing.T) {
	msg := `<?xml version="1.0"?><SOAP-ENV:Envelope><SOAP-ENV:Body><d:ProbeMatches><d:ProbeMatch>
<d:Scopes>onvif://www.onvif.org/type/video_encoder onvif://www.onvif.org/name/Camara-ejemplo onvif://www.onvif.org/hardware/HW%20123</d:Scopes>
<d:XAddrs>http://169.254.1.1/onvif/device_service http://192.0.2.18:888/onvif/device_service</d:XAddrs>
</d:ProbeMatch></d:ProbeMatches></SOAP-ENV:Body></SOAP-ENV:Envelope>`
	d, ok := parseProbeMatch([]byte(msg), "192.0.2.18")
	if !ok {
		t.Fatal("no reconoce la respuesta")
	}
	if d.XAddr != "http://192.0.2.18:888/onvif/device_service" || d.Name != "Camara-ejemplo" || d.Hardware != "HW 123" {
		t.Fatalf("%+v", d)
	}
	if onvifPort(d.XAddr) != 888 {
		t.Fatal("puerto ONVIF mal leído")
	}
	if _, ok := parseProbeMatch([]byte("<Hello/>"), "203.0.113.4"); ok {
		t.Fatal("acepta mensajes que no son ProbeMatch")
	}
}

func TestListOnlyCameras(t *testing.T) {
	r := newResults()
	r.port("192.168.99.1", 80)  // router: solo web
	r.port("192.168.99.9", 554) // cámara RTSP
	r.onvif(onvifDevice{IP: "192.168.99.200", XAddr: "http://192.168.99.200:888/onvif/device_service"})
	got := r.list()
	b, _ := json.Marshal(got)
	if len(got) != 2 || strings.Contains(string(b), "192.168.99.1\"") || strings.Contains(string(b), "null") {
		t.Fatalf("lista inesperada: %s", b)
	}
	if got[0].Suggest.Kind != "rtsp" || got[1].Suggest.Kind != "onvif" || got[1].Suggest.Port != 888 {
		t.Fatalf("sugerencias inesperadas: %s", b)
	}
}

package camera

import (
	"strings"
	"testing"

	"github.com/lucas-maceda/picameras/internal/store"
)

func TestSourcesEscapeCredentials(t *testing.T) {
	c := store.Camera{Kind: KindRTSP, Host: "192.0.2.10", Path: "/stream1", Username: "admin"}
	main, sub, err := Sources(c, `p@ss#word exec:rm -rf /`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(main, "# ") {
		t.Fatalf("la contraseña rompe la fuente: %s", main)
	}
	if !strings.HasPrefix(main, "rtsp://admin:") || !strings.HasSuffix(main, "@192.0.2.10:554/stream1") {
		t.Fatalf("fuente inesperada: %s", main)
	}
	if sub != "" {
		t.Fatalf("sin ruta secundaria no debe haber sub: %s", sub)
	}
}

func TestYiHackSources(t *testing.T) {
	main, sub, err := Sources(store.Camera{Kind: KindYiHack, Host: "192.0.2.11"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if main != "rtsp://192.0.2.11:554/ch0_0.h264" || sub != "rtsp://192.0.2.11:554/ch0_1.h264" {
		t.Fatalf("got %s %s", main, sub)
	}
}

func TestDahuaQueryKept(t *testing.T) {
	c := store.Camera{Kind: KindRTSP, Host: "cam.local", Path: "/cam/realmonitor?channel=1&subtype=0"}
	main, _, err := Sources(c, "")
	if err != nil {
		t.Fatal(err)
	}
	if main != "rtsp://cam.local:554/cam/realmonitor?channel=1&subtype=0" {
		t.Fatalf("got %s", main)
	}
}

func TestNormalizeRejectsInjection(t *testing.T) {
	bad := []Input{
		{Name: "x", Kind: "exec", Host: "192.0.2.1"},
		{Name: "x", Kind: KindRTSP, Host: "exec:calc", Path: "/a"},
		{Name: "x", Kind: KindRTSP, Host: "192.0.2.1/evil", Path: "/a"},
		{Name: "x", Kind: KindRTSP, Host: "user@192.0.2.1", Path: "/a"},
		{Name: "x", Kind: KindRTSP, Host: "192.0.2.1", Path: "/a#input=evil"},
		{Name: "x", Kind: KindRTSP, Host: "192.0.2.1", Path: "/a b"},
		{Name: "x", Kind: KindRTSP, Host: "192.0.2.1", Path: "/a@b"},
		{Name: "x", Kind: KindRTSP, Host: "192.0.2.1", Path: ""},
		{Name: "", Kind: KindRTSP, Host: "192.0.2.1", Path: "/a"},
		{Name: "x\x00", Kind: KindRTSP, Host: "192.0.2.1", Path: "/a"},
		{Name: "x", Kind: KindRTSP, Host: "192.0.2.1", Path: "/a", Port: 70000},
	}
	for i, in := range bad {
		if _, err := Normalize(in); err == nil {
			t.Errorf("caso %d aceptado y no debería: %+v", i, in)
		}
	}
	good := Input{Name: "Entrada", Kind: KindRTSP, Host: "[fd00::5]", Path: "stream1"}
	c, err := Normalize(good)
	if err != nil {
		t.Fatal(err)
	}
	if c.Host != "fd00::5" || c.Path != "/stream1" || c.Site != "Casa" {
		t.Fatalf("normalización inesperada: %+v", c)
	}
	main, _, _ := Sources(c, "")
	if main != "rtsp://[fd00::5]:554/stream1" {
		t.Fatalf("IPv6 mal formada: %s", main)
	}
}

func TestRedact(t *testing.T) {
	in := `error rtsp://admin:secreta@192.0.2.1:554/x and onvif://u:p@h`
	out := Redact(in)
	if strings.Contains(out, "secreta") || strings.Contains(out, "u:p@") {
		t.Fatalf("no se han tapado las credenciales: %s", out)
	}
}

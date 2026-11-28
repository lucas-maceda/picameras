package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lucas-maceda/picameras/internal/store"
)

type fakeEngine struct {
	mu      sync.Mutex
	streams map[string]string
}

func (f *fakeEngine) SetStream(name, src string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if src == "" {
		delete(f.streams, name)
	} else {
		f.streams[name] = src
	}
	return nil
}
func (f *fakeEngine) AddTemporary(name, src string) error { return f.SetStream(name, src) }
func (f *fakeEngine) RemoveTemporary(name string)         { _ = f.SetStream(name, "") }
func (f *fakeEngine) Frame(context.Context, string) ([]byte, string, error) {
	return []byte("mp4"), "video/mp4", nil
}
func (f *fakeEngine) WebSocketProxy(stream string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ws:"+stream) })
}

type env struct {
	t   *testing.T
	srv *Server
	h   http.Handler
	eng *fakeEngine
	dir string
}

func newEnv(t *testing.T) *env {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	eng := &fakeEngine{streams: map[string]string{}}
	disc := func(context.Context, string) (any, error) { return []any{}, nil }
	srv, err := New(st, eng, disc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return &env{t: t, srv: srv, h: srv.Handler(), eng: eng, dir: dir}
}

type reqOpt func(*http.Request)

func withCookie(c string) reqOpt {
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: cookieName, Value: c}) }
}
func fromIP(ip string) reqOpt { return func(r *http.Request) { r.RemoteAddr = ip } }
func noCSRF() reqOpt         { return func(r *http.Request) { r.Header.Del(csrfHeader) } }
func origin(o string) reqOpt { return func(r *http.Request) { r.Header.Set("Origin", o) } }

func (e *env) do(method, path string, body any, opts ...reqOpt) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, "https://picam.local:8443"+path, rd)
	r.RemoteAddr = "192.168.99.30:50000"
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set(csrfHeader, "1")
	for _, o := range opts {
		o(r)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func sessionFrom(w *httptest.ResponseRecorder) string {
	for _, c := range w.Result().Cookies() {
		if c.Name == cookieName {
			return c.Value
		}
	}
	return ""
}

// setup hace la configuración inicial y devuelve la cookie de sesión.
func (e *env) setup() string {
	w := e.do("POST", "/api/setup", map[string]string{
		"code": e.srv.SetupCode(), "username": "admin-prueba", "password": "contraseña-larga-1",
	})
	if w.Code != 200 {
		e.t.Fatalf("setup: %d %s", w.Code, w.Body)
	}
	return sessionFrom(w)
}

// Ninguna ruta de la API que dé acceso a cámaras o datos responde sin sesión.
func TestProtectedRoutesNeedSession(t *testing.T) {
	e := newEnv(t)
	e.setup()
	routes := []struct{ method, path string }{
		{"POST", "/api/logout"}, {"POST", "/api/password"}, {"GET", "/api/meta"},
		{"GET", "/api/cameras"}, {"POST", "/api/cameras"}, {"PUT", "/api/cameras/abc"},
		{"DELETE", "/api/cameras/abc"}, {"POST", "/api/cameras/test"}, {"GET", "/api/cameras/abc/live"},
		{"POST", "/api/discover"},
	}
	for _, rt := range routes {
		for _, cookie := range []string{"", "token-inventado"} {
			w := e.do(rt.method, rt.path, map[string]string{}, withCookie(cookie), origin("https://picam.local:8443"))
			if w.Code != http.StatusUnauthorized {
				t.Errorf("%s %s con cookie %q: %d (esperaba 401)", rt.method, rt.path, cookie, w.Code)
			}
		}
	}
}

func TestSetupNeedsCodeAndOnlyOnce(t *testing.T) {
	e := newEnv(t)
	code := e.srv.SetupCode()
	if code == "" {
		t.Fatal("sin código de configuración")
	}
	data, err := os.ReadFile(filepath.Join(e.dir, SetupFile))
	if err != nil || strings.TrimSpace(string(data)) != code {
		t.Fatal("el código no está en setup-code.txt")
	}
	w := e.do("POST", "/api/setup", map[string]string{"code": "AAAA-BBBB-CCCC-DDDD", "username": "x", "password": "contraseña-larga-1"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("código malo aceptado: %d", w.Code)
	}
	e.setup()
	if _, err := os.Stat(filepath.Join(e.dir, SetupFile)); !os.IsNotExist(err) {
		t.Fatal("setup-code.txt sigue ahí tras configurar")
	}
	w = e.do("POST", "/api/setup", map[string]string{"code": code, "username": "intruso", "password": "contraseña-larga-2"})
	if w.Code != http.StatusConflict {
		t.Fatalf("segunda configuración aceptada: %d", w.Code)
	}
}

func TestSetupBruteForceBlocked(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 5; i++ {
		e.do("POST", "/api/setup", map[string]string{"code": "AAAA-BBBB-CCCC-DDDD", "username": "x", "password": "contraseña-larga-1"})
	}
	w := e.do("POST", "/api/setup", map[string]string{"code": e.srv.SetupCode(), "username": "x", "password": "contraseña-larga-1"})
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("tras 5 fallos sigue probando códigos: %d", w.Code)
	}
}

func TestLoginAndLockout(t *testing.T) {
	e := newEnv(t)
	e.setup()
	for i := 0; i < 5; i++ {
		w := e.do("POST", "/api/login", map[string]string{"username": "admin-prueba", "password": "mala-mala-mala"})
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("intento %d: %d", i, w.Code)
		}
	}
	w := e.do("POST", "/api/login", map[string]string{"username": "admin-prueba", "password": "contraseña-larga-1"})
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("no bloquea tras 5 fallos: %d", w.Code)
	}
	// Otra IP de la red sí puede entrar.
	w = e.do("POST", "/api/login", map[string]string{"username": "admin-prueba", "password": "contraseña-larga-1"}, fromIP("192.168.99.31:1"))
	if w.Code != 200 || sessionFrom(w) == "" {
		t.Fatalf("login bueno rechazado: %d", w.Code)
	}
	c := w.Result().Cookies()[0]
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || !strings.HasPrefix(c.Name, "__Host-") {
		t.Fatalf("cookie poco segura: %+v", c)
	}
}

func TestPublicIPRejected(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{"/", "/api/state", "/app.js"} {
		w := e.do("GET", path, nil, fromIP("203.0.113.5:1"))
		if w.Code != http.StatusForbidden {
			t.Errorf("%s desde IP pública: %d", path, w.Code)
		}
	}
}

func TestCSRF(t *testing.T) {
	e := newEnv(t)
	tok := e.setup()
	in := map[string]any{"name": "Entrada", "kind": "rtsp", "host": "192.0.2.10", "path": "/stream1"}
	if w := e.do("POST", "/api/cameras", in, withCookie(tok), noCSRF()); w.Code != http.StatusForbidden {
		t.Fatalf("sin cabecera anti-CSRF: %d", w.Code)
	}
	if w := e.do("POST", "/api/cameras", in, withCookie(tok), origin("https://malo.example")); w.Code != http.StatusForbidden {
		t.Fatalf("con Origin ajeno: %d", w.Code)
	}
	if w := e.do("POST", "/api/cameras", in, withCookie(tok), origin("https://picam.local:8443")); w.Code != http.StatusCreated {
		t.Fatalf("petición buena rechazada: %d %s", w.Code, w.Body)
	}
}

func TestCameraPasswordNeverLeaks(t *testing.T) {
	e := newEnv(t)
	tok := e.setup()
	const secret = "MiClaveDeCamara#2026"
	in := map[string]any{"name": "Patio", "site": "Sitio B", "kind": "rtsp", "host": "192.168.99.20",
		"path": "/stream1", "subPath": "/stream2", "username": "admin", "password": secret}
	w := e.do("POST", "/api/cameras", in, withCookie(tok))
	if w.Code != http.StatusCreated {
		t.Fatalf("alta: %d %s", w.Code, w.Body)
	}
	var cam map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &cam)
	id := cam["id"].(string)

	list := e.do("GET", "/api/cameras", nil, withCookie(tok))
	if strings.Contains(list.Body.String(), secret) || strings.Contains(list.Body.String(), "passwordEnc") {
		t.Fatal("la API devuelve la contraseña de la cámara")
	}
	state, _ := os.ReadFile(filepath.Join(e.dir, "state.json"))
	if bytes.Contains(state, []byte(secret)) {
		t.Fatal("state.json guarda la contraseña en claro")
	}
	e.eng.mu.Lock()
	src := e.eng.streams[id]
	sub := e.eng.streams[id+"_sub"]
	e.eng.mu.Unlock()
	if !strings.Contains(src, "MiClaveDeCamara%232026@192.168.99.20:554/stream1") || !strings.HasSuffix(sub, "/stream2") {
		t.Fatalf("fuente para go2rtc inesperada: %s / %s", src, sub)
	}

	// Editar sin tocar la contraseña la conserva.
	in["password"] = ""
	in["keepPassword"] = true
	in["name"] = "Patio"
	if w := e.do("PUT", "/api/cameras/"+id, in, withCookie(tok)); w.Code != 200 {
		t.Fatalf("edición: %d %s", w.Code, w.Body)
	}
	e.eng.mu.Lock()
	src2 := e.eng.streams[id]
	e.eng.mu.Unlock()
	if src2 != src {
		t.Fatalf("la edición ha perdido la contraseña: %s", src2)
	}

	// Vídeo en directo: hace falta Origin propio, y la calidad baja se elige con q=sub.
	if w := e.do("GET", "/api/cameras/"+id+"/live", nil, withCookie(tok)); w.Code != http.StatusForbidden {
		t.Fatalf("WebSocket sin Origin aceptado: %d", w.Code)
	}
	w = e.do("GET", "/api/cameras/"+id+"/live?q=sub", nil, withCookie(tok), origin("https://picam.local:8443"))
	if w.Body.String() != "ws:"+id+"_sub" {
		t.Fatalf("stream inesperado: %s", w.Body)
	}

	if w := e.do("DELETE", "/api/cameras/"+id, nil, withCookie(tok)); w.Code != 200 {
		t.Fatalf("borrado: %d", w.Code)
	}
	e.eng.mu.Lock()
	n := len(e.eng.streams)
	e.eng.mu.Unlock()
	if n != 0 {
		t.Fatal("al borrar la cámara quedan streams en el motor")
	}
}

func TestUnknownJSONFieldsRejected(t *testing.T) {
	e := newEnv(t)
	tok := e.setup()
	in := map[string]any{"name": "x", "kind": "rtsp", "host": "192.0.2.1", "path": "/a", "src": "exec:calc"}
	if w := e.do("POST", "/api/cameras", in, withCookie(tok)); w.Code != http.StatusBadRequest {
		t.Fatalf("campo desconocido aceptado: %d", w.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	e := newEnv(t)
	w := e.do("GET", "/", nil)
	if w.Code != 200 {
		t.Fatalf("index: %d", w.Code)
	}
	h := w.Header()
	if !strings.Contains(h.Get("Content-Security-Policy"), "frame-ancestors 'none'") || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("faltan cabeceras de seguridad: %v", h)
	}
}

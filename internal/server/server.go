// Package server es la única puerta de entrada a PiCameras: interfaz web, API y vídeo, todo detrás de la misma
// autenticación.
package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/lucas-maceda/picameras/internal/auth"
	"github.com/lucas-maceda/picameras/internal/camera"
	"github.com/lucas-maceda/picameras/internal/netguard"
	"github.com/lucas-maceda/picameras/internal/store"
)

//go:embed web
var webFS embed.FS

// Engine es lo que el servidor necesita del motor de vídeo.
type Engine interface {
	SetStream(name, src string) error
	AddTemporary(name, src string) error
	RemoveTemporary(name string)
	Frame(ctx context.Context, name string) ([]byte, string, error)
	WebSocketProxy(stream string) http.Handler
}

// Discoverer busca cámaras (se inyecta para poder probar el servidor sin red).
type Discoverer func(ctx context.Context, prefix string) (any, error)

const (
	cookieName = "__Host-picameras"
	sessionTTL = 30 * 24 * time.Hour
	csrfHeader = "X-PiCameras"
	SetupFile  = "setup-code.txt"
)

// Server reúne todo.
type Server struct {
	st       *store.Store
	eng      Engine
	discover Discoverer
	log      *slog.Logger
	sessions *auth.Sessions
	limiter  *auth.Limiter

	setupMu   sync.Mutex
	setupCode string

	discoverMu sync.Mutex
}

// New crea el servidor. Si no hay usuarios, genera el código de configuración inicial.
func New(st *store.Store, eng Engine, disc Discoverer, log *slog.Logger) (*Server, error) {
	s := &Server{
		st:       st,
		eng:      eng,
		discover: disc,
		log:      log,
		sessions: auth.NewSessions(sessionTTL),
		limiter:  auth.NewLimiter(5, 15*time.Minute, 15*time.Minute),
	}
	if !st.HasUsers() {
		s.setupCode = auth.NewSetupCode()
		if err := store.WriteFileAtomic(filepath.Join(st.Dir(), SetupFile), []byte(s.setupCode+"\n")); err != nil {
			return nil, err
		}
	} else {
		_ = os.Remove(filepath.Join(st.Dir(), SetupFile))
	}
	return s, nil
}

// SetupCode devuelve el código de configuración inicial (vacío si ya está configurado).
func (s *Server) SetupCode() string {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	return s.setupCode
}

// SyncCameras da de alta en el motor todas las cámaras guardadas.
func (s *Server) SyncCameras() {
	for _, c := range s.st.Cameras() {
		if err := s.pushCamera(c); err != nil {
			s.log.Warn("cámara sin cargar en el motor", "camera", c.Name, "err", camera.Redact(err.Error()))
		}
	}
}

func (s *Server) pushCamera(c store.Camera) error {
	pass, err := s.st.Decrypt(c.PasswordEnc)
	if err != nil {
		return err
	}
	main, sub, err := camera.Sources(c, pass)
	if err != nil {
		return err
	}
	errMain := s.eng.SetStream(c.ID, main)
	errSub := s.eng.SetStream(c.ID+"_sub", sub)
	return errors.Join(errMain, errSub)
}

// Handler devuelve el http.Handler completo con todos los filtros.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Interfaz web: ficheros públicos (el código es público de todas formas); no contienen datos.
	sub, _ := fs.Sub(webFS, "web")
	static := http.FileServer(http.FS(sub))
	mux.Handle("/", noCache(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "método no permitido", http.StatusMethodNotAllowed)
			return
		}
		static.ServeHTTP(w, r)
	})))

	// API sin sesión.
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("POST /api/setup", s.handleSetup)
	mux.HandleFunc("POST /api/login", s.handleLogin)

	// API con sesión.
	mux.Handle("POST /api/logout", s.requireAuth(http.HandlerFunc(s.handleLogout)))
	mux.Handle("POST /api/password", s.requireAuth(http.HandlerFunc(s.handlePassword)))
	mux.Handle("GET /api/meta", s.requireAuth(http.HandlerFunc(s.handleMeta)))
	mux.Handle("GET /api/cameras", s.requireAuth(http.HandlerFunc(s.handleListCameras)))
	mux.Handle("POST /api/cameras", s.requireAuth(http.HandlerFunc(s.handleCreateCamera)))
	mux.Handle("PUT /api/cameras/{id}", s.requireAuth(http.HandlerFunc(s.handleUpdateCamera)))
	mux.Handle("DELETE /api/cameras/{id}", s.requireAuth(http.HandlerFunc(s.handleDeleteCamera)))
	mux.Handle("POST /api/cameras/test", s.requireAuth(http.HandlerFunc(s.handleTestCamera)))
	mux.Handle("GET /api/cameras/{id}/live", s.requireAuth(http.HandlerFunc(s.handleLive)))
	mux.Handle("POST /api/discover", s.requireAuth(http.HandlerFunc(s.handleDiscover)))

	// Cualquier otra ruta de la API no existe.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		jsonError(w, http.StatusNotFound, "no existe")
	})

	var h http.Handler = mux
	h = s.csrf(h)
	h = securityHeaders(h)
	h = s.lanOnly(h)
	h = s.recoverer(h)
	return h
}

// ---- filtros ----

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.Error("error interno", "path", r.URL.Path, "panic", v)
				jsonError(w, http.StatusInternalServerError, "error interno")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// lanOnly rechaza cualquier conexión que no venga de la red local o de una VPN privada.
func (s *Server) lanOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !netguard.AllowedRemoteAddr(r.RemoteAddr) {
			s.log.Warn("conexión rechazada desde fuera de la red local", "remote", r.RemoteAddr)
			http.Error(w, "PiCameras solo atiende a la red local", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; "+
			"img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self'; object-src 'none'; "+
			"base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	return o != "" && o == "https://"+r.Host
}

// csrf: toda petición que cambia algo tiene que traer la cabecera propia (un formulario de otra web no puede
// ponerla sin permiso CORS, y no damos ninguno) y, si trae Origin, que sea el nuestro.
func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
		default:
			if r.Header.Get(csrfHeader) != "1" || (r.Header.Get("Origin") != "" && !sameOrigin(r)) {
				jsonError(w, http.StatusForbidden, "petición rechazada")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type ctxKey struct{}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil {
			jsonError(w, http.StatusUnauthorized, "inicia sesión")
			return
		}
		user, ok := s.sessions.User(c.Value)
		if !ok {
			jsonError(w, http.StatusUnauthorized, "la sesión ha caducado")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, user)))
	})
}

// ---- utilidades ----

func jsonError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		jsonError(w, http.StatusUnsupportedMediaType, "se espera JSON")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		jsonError(w, http.StatusBadRequest, "datos no válidos")
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		jsonError(w, http.StatusBadRequest, "datos no válidos")
		return false
	}
	return true
}

func (s *Server) setSessionCookie(w http.ResponseWriter, tok string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    tok,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)

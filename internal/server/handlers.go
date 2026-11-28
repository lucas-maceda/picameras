package server

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/lucas-maceda/picameras/internal/auth"
	"github.com/lucas-maceda/picameras/internal/camera"
	"github.com/lucas-maceda/picameras/internal/store"
)

// ---- sesión y configuración inicial ----

func (s *Server) currentUser(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	u, _ := s.sessions.User(c.Value)
	return u
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"needsSetup": s.SetupCode() != "",
		"user":       s.currentUser(r),
	})
}

func (s *Server) tooManyAttempts(w http.ResponseWriter, r *http.Request) bool {
	if blocked, d := s.limiter.Blocked(r.RemoteAddr); blocked {
		mins := int(math.Ceil(d.Minutes()))
		w.Header().Set("Retry-After", fmt.Sprint(int(d.Seconds())))
		jsonError(w, http.StatusTooManyRequests, fmt.Sprintf("demasiados intentos; espera %d min", mins))
		return true
	}
	return false
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code     string `json:"code"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) || s.tooManyAttempts(w, r) {
		return
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if s.setupCode == "" {
		jsonError(w, http.StatusConflict, "PiCameras ya está configurado")
		return
	}
	if !auth.SetupCodeEqual(in.Code, s.setupCode) {
		s.limiter.Fail(r.RemoteAddr)
		jsonError(w, http.StatusUnauthorized, "el código de configuración no es correcto")
		return
	}
	if !usernameRe.MatchString(in.Username) {
		jsonError(w, http.StatusBadRequest, "usuario: de 1 a 32 letras, números, punto, guion o guion bajo")
		return
	}
	if err := auth.ValidatePassword(in.Password); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "error interno")
		return
	}
	if err := s.st.CreateFirstUser(store.User{Name: in.Username, Hash: hash}); err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	s.limiter.Success(r.RemoteAddr)
	s.setupCode = ""
	_ = os.Remove(filepath.Join(s.st.Dir(), SetupFile))
	s.log.Info("configuración inicial hecha", "user", in.Username)
	s.setSessionCookie(w, s.sessions.Create(in.Username))
	writeJSON(w, http.StatusOK, map[string]string{"user": in.Username})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) || s.tooManyAttempts(w, r) {
		return
	}
	u, ok := s.st.User(in.Username)
	hash := ""
	if ok {
		hash = u.Hash
	}
	if len(in.Password) > auth.MaxPasswordLen || !auth.CheckPassword(in.Password, hash) {
		s.limiter.Fail(r.RemoteAddr)
		s.log.Warn("inicio de sesión fallido", "remote", r.RemoteAddr)
		jsonError(w, http.StatusUnauthorized, "usuario o contraseña incorrectos")
		return
	}
	s.limiter.Success(r.RemoteAddr)
	s.setSessionCookie(w, s.sessions.Create(u.Name))
	writeJSON(w, http.StatusOK, map[string]string{"user": u.Name})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.sessions.Delete(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: true, SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !readJSON(w, r, &in) || s.tooManyAttempts(w, r) {
		return
	}
	user := r.Context().Value(ctxKey{}).(string)
	u, ok := s.st.User(user)
	if !ok || !auth.CheckPassword(in.Current, u.Hash) {
		s.limiter.Fail(r.RemoteAddr)
		jsonError(w, http.StatusUnauthorized, "la contraseña actual no es correcta")
		return
	}
	if err := auth.ValidatePassword(in.New); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := auth.HashPassword(in.New)
	if err != nil || s.st.SetUserHash(user, hash) != nil {
		jsonError(w, http.StatusInternalServerError, "error interno")
		return
	}
	c, _ := r.Cookie(cookieName)
	s.sessions.DeleteUser(user, c.Value)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- cámaras ----

type cameraView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Site        string `json:"site"`
	Kind        string `json:"kind"`
	Host        string `json:"host"`
	Port        int    `json:"port,omitempty"`
	Path        string `json:"path,omitempty"`
	SubPath     string `json:"subPath,omitempty"`
	Username    string `json:"username,omitempty"`
	HasPassword bool   `json:"hasPassword"`
	HasSub      bool   `json:"hasSub"`
}

func view(c store.Camera) cameraView {
	return cameraView{
		ID: c.ID, Name: c.Name, Site: c.Site, Kind: c.Kind, Host: c.Host, Port: c.Port, Path: c.Path,
		SubPath: c.SubPath, Username: c.Username, HasPassword: c.PasswordEnc != "",
		HasSub: c.SubPath != "" || c.Kind == camera.KindYiHack,
	}
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"kinds": camera.Kinds, "presets": camera.Presets})
}

func (s *Server) handleListCameras(w http.ResponseWriter, r *http.Request) {
	out := []cameraView{}
	for _, c := range s.st.Cameras() {
		out = append(out, view(c))
	}
	writeJSON(w, http.StatusOK, out)
}

// buildCamera valida el formulario y resuelve la contraseña (nueva o la guardada).
func (s *Server) buildCamera(in camera.Input, existing *store.Camera) (store.Camera, string, error) {
	c, err := camera.Normalize(in)
	if err != nil {
		return c, "", err
	}
	pass := in.Password
	if pass == "" && in.KeepPassword && existing != nil {
		if pass, err = s.st.Decrypt(existing.PasswordEnc); err != nil {
			return c, "", err
		}
	}
	if c.PasswordEnc, err = s.st.Encrypt(pass); err != nil {
		return c, "", err
	}
	return c, pass, nil
}

func (s *Server) handleCreateCamera(w http.ResponseWriter, r *http.Request) {
	var in camera.Input
	if !readJSON(w, r, &in) {
		return
	}
	if len(s.st.Cameras()) >= 64 {
		jsonError(w, http.StatusBadRequest, "como mucho 64 cámaras")
		return
	}
	c, _, err := s.buildCamera(in, nil)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	c.ID = auth.RandomID()
	if err := s.st.PutCamera(c); err != nil {
		jsonError(w, http.StatusInternalServerError, "no se pudo guardar")
		return
	}
	if err := s.pushCamera(c); err != nil {
		s.log.Warn("cámara guardada pero el motor no la acepta", "err", camera.Redact(err.Error()))
	}
	writeJSON(w, http.StatusCreated, view(c))
}

func (s *Server) handleUpdateCamera(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	old, ok := s.st.Camera(id)
	if !ok {
		jsonError(w, http.StatusNotFound, "no existe esa cámara")
		return
	}
	var in camera.Input
	if !readJSON(w, r, &in) {
		return
	}
	c, _, err := s.buildCamera(in, &old)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	c.ID = id
	if err := s.st.PutCamera(c); err != nil {
		jsonError(w, http.StatusInternalServerError, "no se pudo guardar")
		return
	}
	if err := s.pushCamera(c); err != nil {
		s.log.Warn("cámara guardada pero el motor no la acepta", "err", camera.Redact(err.Error()))
	}
	writeJSON(w, http.StatusOK, view(c))
}

func (s *Server) handleDeleteCamera(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.st.DeleteCamera(id); err != nil {
		jsonError(w, http.StatusNotFound, "no existe esa cámara")
		return
	}
	_ = s.eng.SetStream(id, "")
	_ = s.eng.SetStream(id+"_sub", "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTestCamera(w http.ResponseWriter, r *http.Request) {
	var in struct {
		camera.Input
		ID string `json:"id"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	var existing *store.Camera
	if in.ID != "" {
		if c, ok := s.st.Camera(in.ID); ok {
			existing = &c
		}
	}
	c, pass, err := s.buildCamera(in.Input, existing)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	src, _, err := camera.Sources(c, pass)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := "test_" + auth.RandomID()
	if err := s.eng.AddTemporary(name, src); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": camera.Redact(err.Error())})
		return
	}
	defer s.eng.RemoveTemporary(name)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	frame, ctype, err := s.eng.Frame(ctx, name)
	if err != nil {
		msg := camera.Redact(err.Error())
		if errors.Is(err, context.DeadlineExceeded) {
			msg = "la cámara no ha respondido en 15 s"
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": msg})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "frame": base64.StdEncoding.EncodeToString(frame), "contentType": ctype,
	})
}

// handleLive abre el vídeo en directo de una cámara (WebSocket hacia go2rtc).
func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	// Los navegadores siempre mandan Origin en un WebSocket: tiene que ser el nuestro.
	if !sameOrigin(r) {
		jsonError(w, http.StatusForbidden, "origen no permitido")
		return
	}
	c, ok := s.st.Camera(r.PathValue("id"))
	if !ok {
		jsonError(w, http.StatusNotFound, "no existe esa cámara")
		return
	}
	stream := c.ID
	if r.URL.Query().Get("q") == "sub" && view(c).HasSub {
		stream += "_sub"
	}
	s.eng.WebSocketProxy(stream).ServeHTTP(w, r)
}

func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Prefix string `json:"prefix"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !s.discoverMu.TryLock() {
		jsonError(w, http.StatusConflict, "ya hay una búsqueda en marcha")
		return
	}
	defer s.discoverMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	found, err := s.discover(ctx, in.Prefix)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	known := map[string]bool{}
	for _, c := range s.st.Cameras() {
		known[c.Host] = true
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": found, "known": known})
}

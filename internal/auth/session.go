package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// RandomToken devuelve n bytes aleatorios en base64url.
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand no funciona: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// RandomID devuelve un identificador hexadecimal aleatorio (para cámaras).
func RandomID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand no funciona: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// NewSetupCode genera el código de configuración inicial: 80 bits, en grupos fáciles de copiar
// (por ejemplo «K7QF-2MZD-W9XA-LP3T»).
func NewSetupCode() string {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand no funciona: " + err.Error())
	}
	s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b) // 16 caracteres
	return s[0:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16]
}

// SetupCodeEqual compara un código introducido con el bueno, sin distinguir mayúsculas ni guiones, en tiempo
// constante.
func SetupCodeEqual(given, want string) bool {
	norm := func(s string) []byte {
		s = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(s))
		return []byte(s)
	}
	g, w := norm(given), norm(want)
	if len(w) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare(g, w) == 1
}

// Sessions guarda las sesiones abiertas en memoria. Solo se guarda el SHA-256 del token: ni siquiera un volcado
// de memoria permite reutilizar una cookie.
type Sessions struct {
	mu  sync.Mutex
	m   map[string]session
	ttl time.Duration
	max int
	now func() time.Time
}

type session struct {
	user    string
	created time.Time
	expires time.Time
}

// NewSessions crea el almacén de sesiones.
func NewSessions(ttl time.Duration) *Sessions {
	return &Sessions{m: map[string]session{}, ttl: ttl, max: 200, now: time.Now}
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

// Create abre una sesión y devuelve el token para la cookie.
func (s *Sessions) Create(user string) string {
	tok := RandomToken(32)
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked(now)
	if len(s.m) >= s.max {
		var oldestK string
		var oldest time.Time
		for k, v := range s.m {
			if oldestK == "" || v.created.Before(oldest) {
				oldestK, oldest = k, v.created
			}
		}
		delete(s.m, oldestK)
	}
	s.m[hashToken(tok)] = session{user: user, created: now, expires: now.Add(s.ttl)}
	return tok
}

// User devuelve el usuario de una sesión válida.
func (s *Sessions) User(tok string) (string, bool) {
	if tok == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[hashToken(tok)]
	if !ok || s.now().After(v.expires) {
		return "", false
	}
	return v.user, true
}

// Delete cierra una sesión.
func (s *Sessions) Delete(tok string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, hashToken(tok))
}

// DeleteUser cierra todas las sesiones de un usuario salvo la indicada (tras cambiar la contraseña).
func (s *Sessions) DeleteUser(user, keepTok string) {
	keep := hashToken(keepTok)
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.m {
		if v.user == user && k != keep {
			delete(s.m, k)
		}
	}
}

func (s *Sessions) gcLocked(now time.Time) {
	for k, v := range s.m {
		if now.After(v.expires) {
			delete(s.m, k)
		}
	}
}

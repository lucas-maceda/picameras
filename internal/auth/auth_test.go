package auth

import (
	"strings"
	"testing"
	"time"
)

func TestPasswordHash(t *testing.T) {
	h, err := HashPassword("una contraseña larga")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("formato inesperado: %s", h)
	}
	if !CheckPassword("una contraseña larga", h) {
		t.Fatal("la contraseña buena no pasa")
	}
	if CheckPassword("otra cosa", h) || CheckPassword("", h) || CheckPassword("una contraseña larga", "") {
		t.Fatal("pasa una contraseña mala")
	}
	h2, _ := HashPassword("una contraseña larga")
	if h == h2 {
		t.Fatal("dos hashes iguales: falta la sal")
	}
}

func TestValidatePassword(t *testing.T) {
	if ValidatePassword("corta") == nil {
		t.Fatal("acepta contraseñas cortas")
	}
	if ValidatePassword(strings.Repeat("a", 200)) == nil {
		t.Fatal("acepta contraseñas enormes")
	}
	if ValidatePassword("suficientemente-larga") != nil {
		t.Fatal("rechaza una válida")
	}
}

func TestSetupCode(t *testing.T) {
	c := NewSetupCode()
	if len(c) != 19 || c == NewSetupCode() {
		t.Fatalf("código raro: %s", c)
	}
	if !SetupCodeEqual(strings.ToLower(strings.ReplaceAll(c, "-", " ")), c) {
		t.Fatal("no tolera minúsculas ni espacios")
	}
	if SetupCodeEqual("", c) || SetupCodeEqual(c, "") || SetupCodeEqual("AAAA-AAAA-AAAA-AAAA", c) {
		t.Fatal("acepta un código malo")
	}
}

func TestSessions(t *testing.T) {
	s := NewSessions(time.Hour)
	now := time.Now()
	s.now = func() time.Time { return now }
	tok := s.Create("ana")
	if u, ok := s.User(tok); !ok || u != "ana" {
		t.Fatal("sesión no encontrada")
	}
	if _, ok := s.User(tok + "x"); ok {
		t.Fatal("token modificado aceptado")
	}
	for k := range s.m {
		if k == tok {
			t.Fatal("se guarda el token en claro")
		}
	}
	other := s.Create("ana")
	s.DeleteUser("ana", tok)
	if _, ok := s.User(other); ok {
		t.Fatal("DeleteUser no cierra las demás sesiones")
	}
	if _, ok := s.User(tok); !ok {
		t.Fatal("DeleteUser cierra la sesión que debía conservar")
	}
	now = now.Add(2 * time.Hour)
	if _, ok := s.User(tok); ok {
		t.Fatal("sesión caducada aceptada")
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter(3, time.Minute, 10*time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if b, _ := l.Blocked("192.168.1.9:1000"); b {
			t.Fatalf("bloqueado antes de tiempo (intento %d)", i)
		}
		l.Fail("192.168.1.9:1000")
	}
	if b, _ := l.Blocked("192.168.1.9:2000"); !b {
		t.Fatal("no bloquea tras 3 fallos")
	}
	// En IPv6 cuenta el /64 entero.
	for i := 0; i < 3; i++ {
		l.Fail("[fd00:1:2:3::" + string(rune('a'+i)) + "]:1")
	}
	if b, _ := l.Blocked("[fd00:1:2:3::ffff]:1"); !b {
		t.Fatal("cambiar de IPv6 dentro del /64 esquiva el bloqueo")
	}
	now = now.Add(11 * time.Minute)
	if b, _ := l.Blocked("192.168.1.9:2000"); b {
		t.Fatal("el bloqueo no caduca")
	}
}

// Package auth: contraseñas (Argon2id), sesiones, código de configuración inicial y límite de intentos.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Parámetros de Argon2id: el mínimo que recomienda OWASP (19 MiB, 2 pasadas). Caben de sobra en una
// Raspberry Pi Zero 2 W y una comprobación tarda unas décimas de segundo.
const (
	argonMemory  = 19 * 1024
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
	saltLen      = 16

	MinPasswordLen = 10
	MaxPasswordLen = 128
)

// slots limita cuántos hashes se calculan a la vez, para que un ataque de fuerza bruta no deje sin memoria a una
// Pi pequeña.
var slots = make(chan struct{}, 2)

// ValidatePassword comprueba la longitud de una contraseña nueva.
func ValidatePassword(p string) error {
	n := utf8.RuneCountInString(p)
	if n < MinPasswordLen {
		return fmt.Errorf("la contraseña debe tener al menos %d caracteres", MinPasswordLen)
	}
	if len(p) > MaxPasswordLen {
		return fmt.Errorf("la contraseña no puede pasar de %d bytes", MaxPasswordLen)
	}
	return nil
}

// HashPassword devuelve el hash Argon2id en formato PHC.
func HashPassword(p string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	slots <- struct{}{}
	key := argon2.IDKey([]byte(p), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	<-slots
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// dummyHash se usa para gastar el mismo tiempo cuando el usuario no existe y no delatar qué usuarios hay.
var dummyHash, _ = HashPassword("picameras-usuario-inexistente")

// CheckPassword compara una contraseña con un hash PHC en tiempo constante. Con hash vacío, compara contra un
// hash de relleno y devuelve false.
func CheckPassword(p, phc string) bool {
	if phc == "" {
		_, _ = verify(p, dummyHash)
		return false
	}
	ok, err := verify(p, phc)
	return err == nil && ok
}

func verify(p, phc string) (bool, error) {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("hash con formato desconocido")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("versión de argon2 no soportada")
	}
	var m uint32
	var t uint32
	var th uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &th); err != nil {
		return false, err
	}
	if m > 256*1024 || t > 10 || th == 0 || th > 8 {
		return false, errors.New("parámetros de argon2 fuera de rango")
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := enc.DecodeString(parts[5])
	if err != nil || len(want) == 0 || len(want) > 64 {
		return false, errors.New("hash inválido")
	}
	slots <- struct{}{}
	got := argon2.IDKey([]byte(p), salt, t, m, th, uint32(len(want)))
	<-slots
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

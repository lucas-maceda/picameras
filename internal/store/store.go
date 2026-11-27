// Package store guarda el estado de la instalación: usuarios, cámaras y la clave que cifra las contraseñas de las
// cámaras. Todo vive en el directorio de datos, fuera del código, con permisos solo para el usuario del servicio.
package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Camera es una cámara dada de alta. La contraseña solo se guarda cifrada.
type Camera struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Site        string `json:"site"`
	Kind        string `json:"kind"`
	Host        string `json:"host"`
	Port        int    `json:"port,omitempty"`
	Path        string `json:"path,omitempty"`
	SubPath     string `json:"subPath,omitempty"`
	Username    string `json:"username,omitempty"`
	PasswordEnc string `json:"passwordEnc,omitempty"`
}

// User es un usuario de PiCameras. Hash es un Argon2id en formato PHC.
type User struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

// State es lo que se guarda en state.json.
type State struct {
	Version int      `json:"version"`
	Users   []User   `json:"users"`
	Cameras []Camera `json:"cameras"`
}

// Store da acceso concurrente y seguro al estado.
type Store struct {
	dir  string
	mu   sync.RWMutex
	st   State
	aead cipher.AEAD
}

const (
	stateFile = "state.json"
	keyFile   = "secret.key"
)

// Open abre (o crea) el directorio de datos.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	_ = os.Chmod(dir, 0o700)

	key, err := loadOrCreateKey(filepath.Join(dir, keyFile))
	if err != nil {
		return nil, fmt.Errorf("clave de la instalación: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	s := &Store{dir: dir, aead: aead, st: State{Version: 1}}
	data, err := os.ReadFile(filepath.Join(dir, stateFile))
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(data, &s.st); err != nil {
			return nil, fmt.Errorf("state.json dañado: %w", err)
		}
	}
	return s, nil
}

// Dir devuelve el directorio de datos.
func (s *Store) Dir() string { return s.dir }

func loadOrCreateKey(path string) ([]byte, error) {
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != 32 {
			return nil, errors.New("secret.key no mide 32 bytes")
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := WriteFileAtomic(path, key); err != nil {
		return nil, err
	}
	return key, nil
}

// WriteFileAtomic escribe un fichero con permisos 0600 sin dejarlo nunca a medias.
func WriteFileAtomic(path string, data []byte) error {
	// CreateTemp crea el fichero con permisos 0600. En Windows no hay permisos POSIX: protege la ACL del
	// directorio de datos, que por defecto está dentro del perfil del usuario.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.st, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(filepath.Join(s.dir, stateFile), data)
}

// Encrypt cifra un secreto con la clave de la instalación (AES-256-GCM).
func (s *Store) Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := s.aead.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt descifra lo que produjo Encrypt.
func (s *Store) Decrypt(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	n := s.aead.NonceSize()
	if len(raw) < n {
		return "", errors.New("secreto cifrado demasiado corto")
	}
	plain, err := s.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", errors.New("no se pudo descifrar (¿secret.key distinta?)")
	}
	return string(plain), nil
}

// ---- usuarios ----

// HasUsers indica si ya se hizo la configuración inicial.
func (s *Store) HasUsers() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.st.Users) > 0
}

// User busca un usuario por nombre.
func (s *Store) User(name string) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.st.Users {
		if u.Name == name {
			return u, true
		}
	}
	return User{}, false
}

// CreateFirstUser crea el primer usuario solo si todavía no hay ninguno.
func (s *Store) CreateFirstUser(u User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.st.Users) > 0 {
		return errors.New("ya hay un usuario")
	}
	s.st.Users = []User{u}
	return s.saveLocked()
}

// SetUserHash cambia la contraseña de un usuario.
func (s *Store) SetUserHash(name, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.st.Users {
		if s.st.Users[i].Name == name {
			s.st.Users[i].Hash = hash
			return s.saveLocked()
		}
	}
	return errors.New("usuario no encontrado")
}

// ResetUsers borra los usuarios (recuperación local con acceso al equipo).
func (s *Store) ResetUsers() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Users = nil
	return s.saveLocked()
}

// ---- cámaras ----

// Cameras devuelve una copia de la lista de cámaras.
func (s *Store) Cameras() []Camera {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Camera(nil), s.st.Cameras...)
}

// Camera busca una cámara por ID.
func (s *Store) Camera(id string) (Camera, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.st.Cameras {
		if c.ID == id {
			return c, true
		}
	}
	return Camera{}, false
}

// PutCamera crea o sustituye una cámara.
func (s *Store) PutCamera(c Camera) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.st.Cameras {
		if s.st.Cameras[i].ID == c.ID {
			s.st.Cameras[i] = c
			return s.saveLocked()
		}
	}
	s.st.Cameras = append(s.st.Cameras, c)
	return s.saveLocked()
}

// DeleteCamera borra una cámara.
func (s *Store) DeleteCamera(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.st.Cameras {
		if s.st.Cameras[i].ID == id {
			s.st.Cameras = append(s.st.Cameras[:i], s.st.Cameras[i+1:]...)
			return s.saveLocked()
		}
	}
	return errors.New("cámara no encontrada")
}

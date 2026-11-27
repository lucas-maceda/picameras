// Package engine arranca y vigila go2rtc, el motor de vídeo, y habla con él.
//
// go2rtc se lanza encerrado (ver SECURITY.md, regla 7):
//   - su API escucha solo en un socket Unix dentro del directorio de datos, sin ningún puerto TCP;
//   - RTSP, WebRTC, RTMP, HomeKit y el resto de servidores van apagados;
//   - solo se cargan los módulos que hacen falta: nunca exec, expr, ffmpeg, echo, ngrok ni pinggy;
//   - la configuración va por línea de comandos y no lleva secretos; las cámaras se dan de alta por la API y
//     solo viven en memoria, así que go2rtc no escribe credenciales en disco.
package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/lucas-maceda/picameras/internal/camera"
)

// Modules son los únicos módulos de go2rtc que se cargan.
var Modules = []string{"api", "ws", "http", "rtsp", "mp4", "onvif"}

// AllowPaths son las únicas rutas de la API de go2rtc que se registran.
var AllowPaths = []string{"/api/ws", "/api/streams", "/api/frame.mp4"}

// Config devuelve la configuración de go2rtc en JSON.
func Config(socket string) string {
	cfg := map[string]any{
		"app": map[string]any{"modules": Modules},
		"api": map[string]any{
			"listen":      "",
			"unix_listen": socket,
			"allow_paths": AllowPaths,
		},
		"rtsp":   map[string]any{"listen": ""},
		"webrtc": map[string]any{"listen": "", "candidates": []string{}},
		"rtmp":   map[string]any{"listen": ""},
		"srtp":   map[string]any{"listen": ""},
		"log":    map[string]any{"format": "text", "level": "info"},
	}
	b, _ := json.Marshal(cfg)
	return string(b)
}

// Engine es el go2rtc en marcha.
type Engine struct {
	bin    string
	runDir string
	socket string
	log    *slog.Logger
	client *http.Client

	mu      sync.Mutex
	streams map[string]string // nombre → fuente, para volver a darlas de alta si go2rtc se reinicia
	ready   chan struct{}
}

// New prepara el motor. bin es la ruta al ejecutable de go2rtc.
func New(bin, dataDir string, log *slog.Logger) (*Engine, error) {
	runDir := filepath.Join(dataDir, "run")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return nil, err
	}
	_ = os.Chmod(runDir, 0o700)
	socket := filepath.Join(runDir, "go2rtc.sock")
	// Los sockets Unix no admiten rutas de más de ~108 caracteres. Si el directorio de datos está muy hondo, el
	// socket va a un directorio temporal recién creado con nombre aleatorio y permisos 0700 (MkdirTemp), así que
	// nadie puede haberlo preparado antes.
	if len(socket) > 100 {
		d, err := os.MkdirTemp("", "picam-")
		if err != nil {
			return nil, err
		}
		socket = filepath.Join(d, "go2rtc.sock")
	}
	e := &Engine{
		bin:     bin,
		runDir:  runDir,
		socket:  socket,
		log:     log,
		streams: map[string]string{},
		ready:   make(chan struct{}),
	}
	e.client = &http.Client{Transport: e.transport(), Timeout: 20 * time.Second}
	return e, nil
}

func (e *Engine) transport() *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", e.socket)
		},
		MaxIdleConns:    16,
		IdleConnTimeout: 60 * time.Second,
	}
}

// FindBinary busca go2rtc: la ruta indicada, junto al ejecutable de PiCameras o en el PATH.
func FindBinary(flagPath string) (string, error) {
	if flagPath != "" {
		if _, err := os.Stat(flagPath); err != nil {
			return "", fmt.Errorf("go2rtc no está en %s", flagPath)
		}
		return flagPath, nil
	}
	name := "go2rtc"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if self, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(self), name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", errors.New("no encuentro go2rtc: ponlo junto a picameras, en el PATH, o indica -go2rtc <ruta> " +
		"(scripts/descargar-go2rtc.sh lo baja y comprueba su firma SHA-256)")
}

// Run lanza go2rtc y lo vuelve a lanzar si se cae, hasta que ctx se cancele.
func (e *Engine) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		start := time.Now()
		err := e.runOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		e.log.Warn("go2rtc se ha parado; lo vuelvo a arrancar", "err", err)
		if time.Since(start) > time.Minute {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (e *Engine) runOnce(ctx context.Context) error {
	_ = os.Remove(e.socket)
	cmd := exec.CommandContext(ctx, e.bin, "-c", Config(e.socket))
	cmd.Dir = e.runDir
	// Entorno mínimo: go2rtc no necesita nada nuestro.
	cmd.Env = minimalEnv()
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	go e.pipeLogs(out)

	if err := e.waitSocket(ctx, 15*time.Second); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	e.restoreStreams()
	e.mu.Lock()
	select {
	case <-e.ready:
	default:
		close(e.ready)
	}
	e.mu.Unlock()
	return cmd.Wait()
}

func minimalEnv() []string {
	var env []string
	for _, k := range []string{"PATH", "SYSTEMROOT", "TZ", "TMPDIR", "TEMP", "TMP"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// pipeLogs pasa los logs de go2rtc a los nuestros, quitando credenciales de cualquier URL.
func (e *Engine) pipeLogs(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		e.log.Info(camera.Redact(sc.Text()), "src", "go2rtc")
	}
}

func (e *Engine) waitSocket(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("unix", e.socket, time.Second)
		if err == nil {
			c.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
	return errors.New("go2rtc no ha abierto su socket a tiempo")
}

// WaitReady espera a que go2rtc esté listo la primera vez.
func (e *Engine) WaitReady(ctx context.Context) error {
	select {
	case <-e.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Engine) restoreStreams() {
	e.mu.Lock()
	s := make(map[string]string, len(e.streams))
	for k, v := range e.streams {
		s[k] = v
	}
	e.mu.Unlock()
	for name, src := range s {
		if err := e.put(name, src); err != nil {
			e.log.Warn("no pude dar de alta la cámara en go2rtc", "stream", name, "err", camera.Redact(err.Error()))
		}
	}
}

// SetStream da de alta (o sustituye) un stream. Una fuente vacía lo borra.
func (e *Engine) SetStream(name, src string) error {
	e.mu.Lock()
	if src == "" {
		delete(e.streams, name)
	} else {
		e.streams[name] = src
	}
	e.mu.Unlock()
	_ = e.del(name)
	if src == "" {
		return nil
	}
	return e.put(name, src)
}

// AddTemporary da de alta un stream que no se recuerda tras un reinicio (para probar una cámara).
func (e *Engine) AddTemporary(name, src string) error { return e.put(name, src) }

// RemoveTemporary lo borra.
func (e *Engine) RemoveTemporary(name string) { _ = e.del(name) }

func (e *Engine) put(name, src string) error {
	q := url.Values{"name": {name}, "src": {src}}
	req, _ := http.NewRequest(http.MethodPut, "http://go2rtc/api/streams?"+q.Encode(), nil)
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	// Sin fichero de configuración, go2rtc crea el stream en memoria y luego avisa de que no puede guardarlo.
	// Eso es justo lo que queremos.
	if resp.StatusCode == http.StatusOK || strings.Contains(string(body), "config file disabled") {
		return nil
	}
	return fmt.Errorf("go2rtc: %s", camera.Redact(strings.TrimSpace(string(body))))
}

func (e *Engine) del(name string) error {
	q := url.Values{"src": {name}}
	req, _ := http.NewRequest(http.MethodDelete, "http://go2rtc/api/streams?"+q.Encode(), nil)
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// Frame pide a go2rtc un fotograma clave en MP4 (sirve para probar una cámara y para las miniaturas).
func (e *Engine) Frame(ctx context.Context, name string) ([]byte, string, error) {
	q := url.Values{"src": {name}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://go2rtc/api/frame.mp4?"+q.Encode(), nil)
	req.Header.Set("Range", "bytes=0-")
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK || len(body) == 0 {
		return nil, "", fmt.Errorf("la cámara no responde: %s", camera.Redact(strings.TrimSpace(string(body))))
	}
	return body, resp.Header.Get("Content-Type"), nil
}

// WebSocketProxy reenvía un WebSocket ya autenticado a go2rtc, para el stream indicado. Solo se reenvía lo
// imprescindible: ni cookies, ni Origin, ni cabeceras de autenticación.
func (e *Engine) WebSocketProxy(stream string) http.Handler {
	tr := e.transport()
	return &proxy{tr: tr, stream: stream}
}

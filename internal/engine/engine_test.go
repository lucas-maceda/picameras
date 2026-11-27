package engine

import (
	"encoding/json"
	"slices"
	"testing"
)

// El motor no puede abrir ningún puerto de red ni cargar módulos que ejecuten comandos o saquen el vídeo a
// internet (SECURITY.md, regla 7).
func TestConfigIsLockedDown(t *testing.T) {
	var cfg map[string]map[string]any
	if err := json.Unmarshal([]byte(Config("/tmp/x.sock")), &cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg["api"]["listen"]; got != "" {
		t.Fatalf("api.listen = %v; debe estar vacío (solo socket Unix)", got)
	}
	if cfg["api"]["unix_listen"] != "/tmp/x.sock" {
		t.Fatal("api.unix_listen no es el socket indicado")
	}
	for _, mod := range []string{"rtsp", "webrtc", "rtmp", "srtp"} {
		if got := cfg[mod]["listen"]; got != "" {
			t.Fatalf("%s.listen = %v; debe estar vacío", mod, got)
		}
	}
	if _, ok := cfg["api"]["username"]; ok {
		t.Fatal("la config no debe llevar credenciales: va por línea de comandos")
	}
	if _, ok := cfg["api"]["origin"]; ok {
		t.Fatal("no se debe abrir CORS en go2rtc")
	}

	mods := Modules
	for _, banned := range []string{"exec", "expr", "ffmpeg", "echo", "ngrok", "pinggy", "webtorrent", "homekit", "hass", "debug", "webrtc", "rtmp", "srtp", "alsa", "v4l2"} {
		if slices.Contains(mods, banned) {
			t.Errorf("módulo prohibido cargado: %s", banned)
		}
	}
	for _, p := range AllowPaths {
		switch p {
		case "/api/ws", "/api/streams", "/api/frame.mp4":
		default:
			t.Errorf("ruta de go2rtc no esperada: %s", p)
		}
	}
}

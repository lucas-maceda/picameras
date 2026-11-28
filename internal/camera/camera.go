// Package camera valida los datos de una cámara y construye la fuente que entiende go2rtc.
//
// La fuente nunca se acepta tal cual del usuario: se construye aquí a partir de campos validados, con un
// esquema de una lista cerrada. Así nadie puede colar fuentes de go2rtc que ejecutan comandos (exec:, ffmpeg:,
// expr:) ni parámetros con «#».
package camera

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/lucas-maceda/picameras/internal/store"
)

// Tipos de cámara que se pueden dar de alta.
const (
	KindRTSP   = "rtsp"   // cualquier cámara con RTSP (Hikvision, Dahua, Reolink, Tapo…)
	KindONVIF  = "onvif"  // cámaras ONVIF: go2rtc pregunta el stream a la cámara
	KindYiHack = "yihack" // cámaras YI con el firmware yi-hack
	KindHTTP   = "http"   // MJPEG o JPEG por HTTP (cámaras antiguas, ESP32-CAM)
)

// Kinds es la lista cerrada de tipos.
var Kinds = []string{KindRTSP, KindONVIF, KindYiHack, KindHTTP}

var (
	hostnameRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$`)
	// Ruta: lo justo para las rutas RTSP y HTTP de las marcas habituales. Sin «#», espacios, comillas, «@» ni «\».
	pathRe = regexp.MustCompile(`^/[A-Za-z0-9/._~\-=&?%,+;:]*$`)
)

// Input es lo que llega del formulario.
type Input struct {
	Name     string `json:"name"`
	Site     string `json:"site"`
	Kind     string `json:"kind"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Path     string `json:"path"`
	SubPath  string `json:"subPath"`
	Username string `json:"username"`
	Password string `json:"password"`
	// KeepPassword: al editar, conservar la contraseña guardada si Password viene vacía.
	KeepPassword bool `json:"keepPassword"`
}

func cleanText(s string, max int, field string) (string, error) {
	s = strings.TrimSpace(s)
	if !utf8.ValidString(s) {
		return "", fmt.Errorf("%s: texto no válido", field)
	}
	if utf8.RuneCountInString(s) > max {
		return "", fmt.Errorf("%s: máximo %d caracteres", field, max)
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("%s: caracteres de control no permitidos", field)
		}
	}
	return s, nil
}

// ValidHost acepta una IP (v4 o v6) o un nombre de host.
func ValidHost(h string) bool {
	if net.ParseIP(h) != nil {
		return true
	}
	return len(h) <= 253 && hostnameRe.MatchString(h)
}

func cleanPath(p, field string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", nil
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if len(p) > 256 || !pathRe.MatchString(p) {
		return "", fmt.Errorf("%s: solo letras, números y / . _ ~ - = & ? %% , + ; :", field)
	}
	return p, nil
}

// Normalize valida el formulario y devuelve la cámara lista para guardar (sin la contraseña, que se cifra
// aparte).
func Normalize(in Input) (store.Camera, error) {
	var c store.Camera
	var err error
	if c.Name, err = cleanText(in.Name, 60, "nombre"); err != nil {
		return c, err
	}
	if c.Name == "" {
		return c, errors.New("ponle un nombre a la cámara")
	}
	if c.Site, err = cleanText(in.Site, 40, "ubicación"); err != nil {
		return c, err
	}
	if c.Site == "" {
		c.Site = "Casa"
	}
	switch in.Kind {
	case KindRTSP, KindONVIF, KindYiHack, KindHTTP:
		c.Kind = in.Kind
	default:
		return c, errors.New("tipo de cámara no soportado")
	}
	c.Host = strings.TrimSpace(in.Host)
	c.Host = strings.TrimSuffix(strings.TrimPrefix(c.Host, "["), "]")
	if !ValidHost(c.Host) {
		return c, errors.New("dirección de la cámara no válida (pon una IP o un nombre, sin http:// ni rutas)")
	}
	if in.Port < 0 || in.Port > 65535 {
		return c, errors.New("puerto fuera de rango")
	}
	c.Port = in.Port
	if c.Path, err = cleanPath(in.Path, "ruta"); err != nil {
		return c, err
	}
	if c.SubPath, err = cleanPath(in.SubPath, "ruta secundaria"); err != nil {
		return c, err
	}
	if c.Username, err = cleanText(in.Username, 64, "usuario"); err != nil {
		return c, err
	}
	if len(in.Password) > 128 {
		return c, errors.New("contraseña de la cámara demasiado larga")
	}
	if c.Kind == KindRTSP && c.Path == "" {
		return c, errors.New("falta la ruta RTSP (elige la de tu marca o mira el manual)")
	}
	return c, nil
}

func hostPort(host string, port, def int) string {
	if port == 0 {
		port = def
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func userinfo(user, pass string) *url.Userinfo {
	if user == "" && pass == "" {
		return nil
	}
	if pass == "" {
		return url.User(user)
	}
	return url.UserPassword(user, pass)
}

// Sources construye la fuente principal y, si la hay, la secundaria (calidad baja, para el mosaico).
func Sources(c store.Camera, password string) (main, sub string, err error) {
	if !ValidHost(c.Host) {
		return "", "", errors.New("host no válido")
	}
	ui := userinfo(c.Username, password)
	build := func(scheme, hp, path string) string {
		u := &url.URL{Scheme: scheme, User: ui, Host: hp}
		if path != "" {
			p, q, _ := strings.Cut(path, "?")
			u.Path = p
			u.RawQuery = q
		}
		return u.String()
	}
	switch c.Kind {
	case KindRTSP:
		hp := hostPort(c.Host, c.Port, 554)
		main = build("rtsp", hp, c.Path)
		if c.SubPath != "" {
			sub = build("rtsp", hp, c.SubPath)
		}
	case KindYiHack:
		hp := hostPort(c.Host, c.Port, 554)
		main = build("rtsp", hp, "/ch0_0.h264")
		sub = build("rtsp", hp, "/ch0_1.h264")
	case KindONVIF:
		main = build("onvif", hostPort(c.Host, c.Port, 80), "")
	case KindHTTP:
		main = build("http", hostPort(c.Host, c.Port, 80), c.Path)
	default:
		return "", "", errors.New("tipo de cámara no soportado")
	}
	for _, s := range []string{main, sub} {
		if strings.ContainsAny(s, "#\n\r\t ") {
			return "", "", errors.New("la fuente contiene caracteres no permitidos")
		}
	}
	return main, sub, nil
}

// Redact quita usuario y contraseña de cualquier URL que aparezca en un texto (para logs y errores).
var redactRe = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.\-]*://)[^/@\s]*@`)

// Redact sustituye las credenciales de las URLs por «***».
func Redact(s string) string {
	return redactRe.ReplaceAllString(s, "${1}***@")
}

// Presets son rutas RTSP habituales por marca, para el formulario.
var Presets = []struct {
	Brand   string `json:"brand"`
	Path    string `json:"path"`
	SubPath string `json:"subPath"`
}{
	{"Hikvision / Annke", "/Streaming/Channels/101", "/Streaming/Channels/102"},
	{"Dahua / Imou / Amcrest", "/cam/realmonitor?channel=1&subtype=0", "/cam/realmonitor?channel=1&subtype=1"},
	{"Reolink", "/h264Preview_01_main", "/h264Preview_01_sub"},
	{"TP-Link Tapo / Vigi", "/stream1", "/stream2"},
	{"Ezviz", "/h264/ch1/main/av_stream", "/h264/ch1/sub/av_stream"},
	{"Foscam", "/videoMain", "/videoSub"},
	{"Uniview", "/unicast/c1/s0/live", "/unicast/c1/s1/live"},
	{"Axis", "/axis-media/media.amp", ""},
	{"Wyze (firmware RTSP)", "/live", ""},
}

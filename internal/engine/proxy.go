package engine

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

type proxy struct {
	tr     http.RoundTripper
	stream string
}

func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rp := &httputil.ReverseProxy{
		Transport: p.tr,
		Rewrite: func(pr *httputil.ProxyRequest) {
			// La URL de destino se construye entera aquí: nada de la petición del navegador pasa a go2rtc salvo
			// el propio WebSocket.
			pr.Out.URL = &url.URL{
				Scheme:   "http",
				Host:     "go2rtc",
				Path:     "/api/ws",
				RawQuery: url.Values{"src": {p.stream}}.Encode(),
			}
			pr.Out.Host = "go2rtc"
			for _, h := range []string{"Cookie", "Origin", "Authorization", "Referer", "X-Forwarded-For"} {
				pr.Out.Header.Del(h)
			}
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "motor de vídeo no disponible", http.StatusBadGateway)
		},
	}
	rp.ServeHTTP(w, r)
}

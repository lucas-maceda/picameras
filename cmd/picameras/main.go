// PiCameras: monitor de cámaras para la red local.
//
//	picameras                       arranca el servidor
//	picameras reset-password        borra el usuario para volver a configurarlo (con el servicio parado)
//	picameras version
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/lucas-maceda/picameras/internal/discover"
	"github.com/lucas-maceda/picameras/internal/engine"
	"github.com/lucas-maceda/picameras/internal/server"
	"github.com/lucas-maceda/picameras/internal/store"
	"github.com/lucas-maceda/picameras/internal/tlsutil"
)

var version = "dev"

func defaultDataDir() string {
	if d := os.Getenv("PICAMERAS_DATA"); d != "" {
		return d
	}
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "picameras")
	}
	return "picameras-data"
}

func main() {
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}

	fs := flag.NewFlagSet("picameras", flag.ExitOnError)
	dataDir := fs.String("data", defaultDataDir(), "directorio de datos (usuarios, cámaras y claves)")
	listen := fs.String("listen", ":8443", "dirección HTTPS en la que escuchar")
	go2rtcPath := fs.String("go2rtc", "", "ruta al ejecutable de go2rtc")
	_ = fs.Parse(args)

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	var err error
	switch cmd {
	case "serve":
		err = serve(log, *dataDir, *listen, *go2rtcPath)
	case "reset-password":
		err = resetPassword(*dataDir)
	case "version":
		fmt.Println("picameras", version)
	default:
		err = fmt.Errorf("orden desconocida %q (usa serve, reset-password o version)", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func resetPassword(dataDir string) error {
	st, err := store.Open(dataDir)
	if err != nil {
		return err
	}
	if err := st.ResetUsers(); err != nil {
		return err
	}
	fmt.Println("Usuario borrado. Arranca PiCameras: te dará un código de configuración nuevo para crear otro.")
	fmt.Println("Las cámaras se conservan.")
	return nil
}

func serve(log *slog.Logger, dataDir, listen, go2rtcPath string) error {
	st, err := store.Open(dataDir)
	if err != nil {
		return err
	}
	bin, err := engine.FindBinary(go2rtcPath)
	if err != nil {
		return err
	}
	eng, err := engine.New(bin, dataDir, log)
	if err != nil {
		return err
	}
	disc := func(ctx context.Context, prefix string) (any, error) {
		return discover.Run(ctx, discover.Options{Prefix: prefix})
	}
	srv, err := server.New(st, eng, disc, log)
	if err != nil {
		return err
	}
	cert, fp, err := tlsutil.LoadOrCreate(dataDir)
	if err != nil {
		return fmt.Errorf("certificado HTTPS: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go eng.Run(ctx)
	go func() {
		wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := eng.WaitReady(wctx); err != nil {
			log.Error("go2rtc no arranca", "err", err)
			return
		}
		srv.SyncCameras()
		log.Info("motor de vídeo listo", "cameras", len(st.Cameras()))
	}()

	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	hs := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelDebug),
	}

	banner(dataDir, port, fp, srv.SetupCode())

	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
	}()
	if err := hs.ServeTLS(ln, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func banner(dataDir, port, fp, setupCode string) {
	var b strings.Builder
	b.WriteString("\n  PiCameras " + version + "\n\n  Ábrelo desde un navegador de tu red:\n")
	urls := tlsutil.LocalURLs(port)
	if len(urls) == 0 {
		urls = []string{"https://localhost:" + port}
	}
	for _, u := range urls {
		b.WriteString("    " + u + "\n")
	}
	b.WriteString("\n  El navegador avisará de que el certificado no es de confianza: es propio de esta instalación.\n")
	b.WriteString("  Comprueba que su huella SHA-256 coincide con esta:\n    " + fp + "\n")
	if setupCode != "" {
		b.WriteString("\n  Código de configuración inicial (te lo pedirá la web para crear tu usuario):\n")
		b.WriteString("    " + setupCode + "\n")
		b.WriteString("  También está en " + filepath.Join(dataDir, server.SetupFile) + "\n")
	}
	b.WriteString("\n")
	fmt.Print(b.String())
}

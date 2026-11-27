# PiCameras

Monitor de cámaras para la red local: open source, seguro por defecto y fácil de configurar para cualquiera.
Funciona en un PC o en una Raspberry Pi y acepta cámaras de distintas marcas y protocolos, también las cámaras
Wi-Fi de consumo que vienen atadas a la app y la nube del fabricante.

> **Estado:** primera versión (v0.1). Busca y muestra cámaras RTSP, ONVIF, YI con yi-hack y MJPEG. Todavía sin
> grabación ni releases publicadas.

## Premisa de seguridad

**El código es público; tus cámaras no.** Tener todo el código de PiCameras delante no da ninguna forma de entrar
en las cámaras de nadie: no hay contraseñas por defecto, ni claves en el repositorio, ni puertas traseras. Toda la
seguridad descansa en secretos que se generan en cada instalación y nunca salen de ella. Las reglas completas y
cómo informar de una vulnerabilidad están en [SECURITY.md](SECURITY.md).

## Qué hace

- **Busca las cámaras solo:** ONVIF (WS-Discovery), barrido de puertos RTSP y reconocimiento de marcas y del
  yi-hack. También en la red de otro sitio unido por VPN.
- **Te ayuda a darlas de alta:** rutas RTSP por marca y un botón *Probar* que enseña un fotograma antes de guardar.
- **Mosaico por ubicaciones** (Casa, Local, Pueblo…) en calidad baja y vista grande en calidad alta, en el
  navegador del PC o del móvil.
- **Seguro sin configurar nada:** HTTPS propio, usuario creado con un código de un solo uso, solo red local,
  límite de intentos y el motor de vídeo (go2rtc) sin un solo puerto abierto.

## Empezar

```sh
go build -o picameras ./cmd/picameras      # Go 1.26 o posterior
sh scripts/descargar-go2rtc.sh .           # en Windows: scripts\descargar-go2rtc.ps1
./picameras
```

Abre la dirección que aparece en la consola, comprueba la huella del certificado y crea tu usuario con el
código de configuración. Para instalarlo en una Raspberry Pi como servicio, mira [docs/instalar.md](docs/instalar.md).

## Documentación

- [Instalar](docs/instalar.md): Raspberry Pi, Windows, macOS y Linux.
- [Cámaras en varios sitios](docs/acceso-remoto.md): unir varios sitios sin abrir puertos.
- [Diseño de la v1](docs/superpowers/specs/2026-11-28-picameras-app-v1-design.md): decisiones y por qué.
- [Investigación inicial (27-nov-2026)](docs/investigacion/2026-11-27-picameras-investigacion-inicial.md): qué
  existe ya y la cámara de referencia (YI Dome Guard YRS.3019).

## Estructura

```
cmd/picameras/        el programa
internal/             auth, camera, discover, engine (go2rtc), netguard, server (+ web/), store, tlsutil
deploy/               servicio de systemd
scripts/              compilar y descargar go2rtc con su SHA-256
docs/                 instalación, acceso remoto, investigación y diseños
```

## Créditos

El vídeo lo mueve [go2rtc](https://github.com/AlexxIT/go2rtc) (MIT), de Alexey Khit. `internal/server/web/vendor/video-rtc.js`
es suyo, sin cambios.

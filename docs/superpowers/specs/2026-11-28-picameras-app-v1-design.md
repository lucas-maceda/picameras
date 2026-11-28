# PiCameras v1: diseño

> Fecha: 28-nov-2026 · Autor: lucas-maceda (con Claude) · Estado: implementado en la rama `feat/app-v1`

## Objetivo

Una primera versión sencilla que funcione bien en una Raspberry Pi barata y en un PC, que encuentre las cámaras
de la red y que cumpla la premisa del proyecto: **tener el código no da ninguna forma de entrar en las cámaras de
nadie** ([SECURITY.md](../../../SECURITY.md)).

El encargo: nada sofisticado, que vaya bien en una Raspberry Pi barata y en un PC y que encuentre todas las cámaras.
Además, tiene que poder juntar las cámaras de varios sitios (otro local, otra vivienda…).

## Cómo estaba

Solo había investigación ([investigacion inicial](../../investigacion/2026-11-27-picameras-investigacion-inicial.md)),
sin código. Su propuesta: go2rtc como motor y una capa propia encima, con descubrimiento, alta guiada, seguridad
por defecto y una web sencilla.

## Decisiones (tomadas por Claude; ninguna es determinante)

| Decisión | Por qué |
|---|---|
| **Go**, un solo binario sin dependencias de sistema | Compila a ARMv6/ARMv7/ARM64/x86 y Windows/macOS sin instalar nada. Pesa unos 8 MB y gasta poca memoria en una Pi Zero 2 W. Es el mismo lenguaje que go2rtc |
| **go2rtc v1.9.15 como proceso aparte**, no como librería | Sus paquetes útiles son `internal/` y no se pueden importar. Como proceso, además, queda encerrado (ver abajo) y se actualiza por separado |
| **go2rtc sin puertos de red: API solo por socket Unix** | Leí su código: con `api.listen: ""` y `unix_listen` no abre TCP, y con `rtsp/webrtc/rtmp/srtp.listen: ""` apaga sus servidores. Comprobado en Windows y Linux: cero sockets de red. Así no le hacen falta ni contraseña ni CORS, y un ataque de *DNS rebinding* contra `localhost:1984` (su puerto por defecto, sin autenticación local) no tiene a qué apuntar |
| **Configuración de go2rtc por `-c '{json}'`, cámaras por la API** | Sin fichero de configuración, go2rtc no guarda los streams en disco (`PatchConfig` devuelve «config file disabled»). Las credenciales de las cámaras solo están cifradas en `state.json` y en la memoria de go2rtc |
| **Lista cerrada de módulos** (`api ws http rtsp mp4 onvif`) y de rutas (`/api/ws /api/streams /api/frame.mp4`) | `exec`, `expr` y `ffmpeg` permiten ejecutar comandos a partir de una fuente. Ni se cargan |
| **La fuente de go2rtc se construye a partir de campos validados**, nunca la escribe el usuario | Lista cerrada de tipos (`rtsp`, `onvif`, `yihack`, `http`), host y ruta con regex, y credenciales escapadas con `url.UserPassword`. Una contraseña con `#` o espacios no rompe ni inyecta nada (está en los tests) |
| **Vídeo solo por MSE sobre el WebSocket de PiCameras** | No hace falta abrir puertos UDP para WebRTC. El WebSocket pasa por la sesión y comprueba el `Origin`. Funciona en Chrome, Edge, Firefox, Safari de escritorio e iOS 17.1 o posterior (ManagedMediaSource) |
| **HTTPS con certificado autofirmado propio de cada instalación**, puerto 8443 | Sin dominio no hay certificado público. Se enseña la huella SHA-256 para compararla |
| **Código de configuración inicial** en vez de «solo desde localhost» | La Pi va sin pantalla. Cambio de la regla 2 de SECURITY.md, explicado allí |
| **Solo IPs privadas, link-local y 100.64/10 (Tailscale)** pueden conectar | Si alguien abre el puerto en el router por error, sigue cerrado |
| **Interfaz en HTML/CSS/JS sin framework ni compilación**, incrustada en el binario | No hay dependencias npm que auditar. Se usa `video-rtc.js` de go2rtc (MIT, copiado sin cambios) |
| **Calidad baja en el mosaico** si la cámara la tiene | Importa para los sitios remotos, con poca subida |
| **Descubrimiento propio:** WS-Discovery (ONVIF) por multicast + barrido TCP de 554/8554/80/8080 + huella HTTP | El barrido también vale para redes remotas por VPN (*Otra red*, hasta /22 y solo rangos privados). En una red real de prueba encontró dos cámaras ONVIF en un puerto no estándar (888) que un barrido de puertos clásico no veía |

## Cambios

```
cmd/picameras/          arranque, banner (URL, huella, código), reset-password
internal/auth/          Argon2id (19 MiB, t=2), sesiones (solo el SHA-256 del token), límite de intentos, código
internal/camera/        validación del formulario → fuente de go2rtc; Redact() para los logs
internal/discover/      WS-Discovery, barrido de puertos, huella HTTP
internal/engine/        lanza y vigila go2rtc, socket Unix, proxy del WebSocket
internal/netguard/      qué IPs pueden conectar
internal/server/        API, filtros (solo LAN, cabeceras, CSRF, sesión) y la web incrustada
internal/store/         state.json (0600), secret.key y cifrado AES-256-GCM
internal/tlsutil/       certificado propio
deploy/picameras.service  systemd con usuario propio y endurecimiento
scripts/                compilar.sh, descargar-go2rtc.sh/.ps1 (SHA-256 fijado)
.github/workflows/ci.yml  vet, test -race, compilación cruzada, govulncheck, gitleaks
```

API (todo JSON y, salvo las tres primeras rutas, todo con sesión):

| Ruta | Para qué |
|---|---|
| `GET /api/state` | ¿Hace falta configurar? ¿Quién soy? |
| `POST /api/setup` | Crear el primer usuario con el código |
| `POST /api/login` | Entrar |
| `POST /api/logout`, `POST /api/password` | Salir y cambiar la contraseña |
| `GET /api/meta` | Tipos de cámara y rutas por marca |
| `GET/POST /api/cameras`, `PUT/DELETE /api/cameras/{id}` | Gestión de cámaras (sin devolver nunca la contraseña) |
| `POST /api/cameras/test` | Probar una cámara antes de guardarla (devuelve un fotograma) |
| `GET /api/cameras/{id}/live[?q=sub]` | WebSocket del vídeo |
| `POST /api/discover` | Buscar cámaras (en la red local o en un rango) |

## Casos límite

- **go2rtc se cae:** se relanza con espera creciente (de 1 a 30 s) y se le vuelven a dar de alta las cámaras.
- **Ruta del socket demasiado larga** (más de 100 caracteres; los sockets Unix admiten unos 108): se crea un
  directorio temporal nuevo con `MkdirTemp` (nombre aleatorio y permisos 0700).
- **Edición sin escribir la contraseña:** se conserva la guardada (`keepPassword`).
- **El RTSP de go2rtc no pide contraseña a las conexiones desde 127.0.0.1.** Solo afecta a las pruebas con una
  cámara falsa en local, no a PiCameras.
- **Una IPv6 que va cambiando** no esquiva el límite de intentos: se cuenta por /64.

## Criterios de aceptación (comprobados)

- [x] `go vet` y `go test ./...` en verde, también con tests de seguridad: todas las rutas exigen sesión, CSRF,
  IPs públicas rechazadas, contraseña nunca en la API, en `state.json` ni en los logs, inyección en la fuente y
  configuración de go2rtc cerrada.
- [x] Compila para linux/arm64, arm v7, arm v6, amd64, windows/amd64 y darwin/arm64.
- [x] De principio a fin con go2rtc real y una cámara RTSP falsa con contraseña: con la contraseña mala sale
  «wrong user/pass»; con la buena, vídeo en directo en el mosaico y en la vista grande (Edge, 1280/1440/390 px).
- [x] go2rtc sin sockets de red (netstat en Windows, `/proc/net` en Linux).
- [x] `descargar-go2rtc.sh` rechaza un binario con la SHA-256 cambiada.

## Pendiente (fuera de la v1)

- Avisar de cámaras sin contraseña o con la de fábrica (regla 9).
- Releases firmadas y binarios publicados.
- PTZ, grabación y detección de movimiento.
- Cámaras con nube propia (Xiaomi, Tuya, Wyze), con los módulos de go2rtc que ya existen y su revisión de
  seguridad.
- Probar en una Pi de verdad y con la YI Dome Guard.

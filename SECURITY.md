# Seguridad en PiCameras

## La premisa: el código es público, tus cámaras no

PiCameras es open source. Cualquiera puede leer todo el código, el historial y la documentación. **Eso no puede
darle a nadie una forma de entrar en las cámaras de quien lo use.**

Es el [principio de Kerckhoffs](https://es.wikipedia.org/wiki/Principios_de_Kerckhoffs): un sistema tiene que
seguir siendo seguro aunque el atacante lo sepa todo de él, salvo la clave. En PiCameras, **toda la seguridad
descansa en secretos que se generan en cada instalación y que nunca salen de ella**. Nada en este repositorio
debe abrir ninguna instalación.

Esta premisa manda sobre cualquier otra decisión del proyecto, también sobre la comodidad. Un cambio que la rompa
no se acepta, aunque haga el programa más fácil de usar.

## Modelo de amenazas

**Damos por hecho que el atacante:**

- tiene el código fuente completo, el historial de Git, los binarios publicados y esta documentación;
- conoce las cámaras compatibles y sus firmwares (incluido el yi-hack y sus valores de fábrica);
- puede estar en internet o **dentro de la misma red local** (un invitado en el Wi-Fi, un dispositivo IoT
  comprometido).

**Y aun así no debe poder:** ver vídeo, hacer snapshots, mover el PTZ, cambiar la configuración, leer las
credenciales de las cámaras ni descubrir qué cámaras hay.

**Queda fuera de nuestro alcance:** quien ya es administrador (root) o tiene acceso físico al equipo donde corre
PiCameras, y las vulnerabilidades del firmware de las propias cámaras. Para lo segundo, ver la regla 9.

## Reglas obligatorias

1. **Ningún secreto en el repositorio.** Ni contraseñas, ni claves, ni tokens, ni certificados privados, ni
   ficheros `.env`, ni siquiera «de prueba» que funcionen contra una instalación real. Los tests usan secretos
   generados en el momento.
2. **Sin credenciales por defecto.** No existe usuario `admin/admin` ni nada parecido. En el primer arranque,
   PiCameras genera un **código de configuración** aleatorio de 80 bits y solo lo enseña en la consola y en un
   fichero (`setup-code.txt`) que solo puede leer quien tenga acceso al equipo. Sin ese código no se puede crear
   el usuario. El código caduca en cuanto se usa, y los intentos fallidos bloquean la IP 15 minutos.
   *Por qué no «solo desde localhost», como decía la primera versión:* una Raspberry Pi suele ir sin pantalla, así
   que la configuración se hace desde otro equipo de la red. El código cumple el mismo propósito (solo configura
   quien tiene acceso físico o de administrador al equipo) sin obligar a conectar un monitor.
3. **Secretos por instalación.** Las claves de sesión, de cifrado y de firma, y el certificado HTTPS se generan
   en el primer arranque con un generador criptográficamente seguro (`crypto/rand`, `secrets` o equivalente). Se
   guardan fuera del directorio del código, con permisos solo para el usuario del servicio.
4. **Contraseñas con hash fuerte.** Las de los usuarios se guardan con Argon2id (o bcrypt si no hay más remedio),
   nunca en claro ni con hashes rápidos (MD5, SHA-1, SHA-256 a secas).
5. **Las credenciales de las cámaras, cifradas en disco** con la clave de la instalación. Nunca salen en logs,
   mensajes de error, respuestas de la API ni volcados de depuración.
6. **Autenticación en todo.** Interfaz web, API, WebSocket, WebRTC, snapshots, HLS/MJPEG y RTSP de salida. No
   hay endpoints «ocultos», «de depuración» ni rutas que se salten la autenticación: si una ruta existe, está en
   el código y cualquiera la conoce.
7. **El motor (go2rtc) nunca se expone directamente.** No abre **ningún** puerto de red: su API escucha solo en
   un socket Unix dentro del directorio de datos (que tiene permisos `0700`), y sus servidores RTSP, WebRTC, RTMP
   y HomeKit van apagados. Solo se cargan los módulos imprescindibles (nunca `exec`, `expr`, `ffmpeg`, `ngrok`…)
   y solo se registran tres rutas de su API. Las cámaras se le dan de alta en memoria: go2rtc no escribe ninguna
   credencial en disco. Todo el vídeo sale por un WebSocket de PiCameras con sesión. Hay un test que comprueba
   esta configuración (`internal/engine/engine_test.go`).
8. **Solo red local por defecto, y con HTTPS.** No se abren puertos a internet (nada de UPnP). Para el acceso
   remoto, documentamos Tailscale o WireGuard. Límite de intentos y bloqueo temporal en el inicio de sesión.
9. **Las cámaras también se blindan.** El asistente obliga a cambiar las contraseñas de fábrica de la cámara (o
   del yi-hack) antes de darla de alta, y recomienda aislar las cámaras en su propia red sin salida a internet.
10. **Sin puertas traseras ni telemetría.** Ni cuentas de soporte, ni claves maestras, ni recuperación remota, ni
    envío de datos a ningún servidor. La recuperación de la contraseña se hace con acceso físico al equipo (un
    comando local), nunca por red.
11. **Cadena de suministro.** Dependencias con versión fijada, escaneo de secretos y de dependencias en cada PR, y
    binarios publicados con su suma de comprobación y firma.

## Estado en la versión actual

| Regla | Estado |
|---|---|
| 1, 3, 4, 5, 6, 7, 8, 10 | Cumplidas y cubiertas por tests (`go test ./...`) |
| 2 | Cumplida con el código de configuración |
| 9 | **Pendiente.** PiCameras no puede cambiar la contraseña de cada marca de cámara. Falta, como mínimo, avisar cuando una cámara entra sin contraseña o con una de fábrica |
| 11 | Parcial: dependencias fijadas, go2rtc descargado con SHA-256 fijado, Actions fijadas por SHA, gitleaks y govulncheck en la CI. **Falta** firmar los binarios cuando haya releases |

**Límites conocidos, para que nadie se lleve a engaño:**

- El certificado HTTPS es autofirmado. La primera vez, el navegador avisa: compara la huella SHA-256 que muestra
  PiCameras al arrancar con la del navegador.
- La clave que cifra las contraseñas de las cámaras vive en el mismo directorio de datos. Protege una copia
  suelta de `state.json`, pero no a quien se lleve el directorio entero (y quien puede hacer eso ya tiene el
  equipo).
- Las sesiones viven en memoria: al reiniciar PiCameras hay que volver a entrar.
- Lo que viaja entre PiCameras y una cámara (RTSP) suele ir sin cifrar, porque así lo hacen casi todas las
  cámaras. Por eso entre sitios distintos se va por Tailscale o WireGuard, nunca abriendo puertos.

## Cómo se revisa

Toda PR que toque autenticación, red, almacenamiento de credenciales o la configuración de go2rtc se revisa
contra estas reglas. La pregunta es siempre la misma: **«Si un atacante tiene este código delante y está en mi
Wi-Fi, ¿puede entrar?»**. Si la respuesta no es un «no» claro, la PR no entra.

## Cómo informar de una vulnerabilidad

**No abras un issue público.** Usa el informe privado de GitHub: pestaña **Security** → **Report a
vulnerability**. Respondemos en cuanto podamos y te damos crédito cuando se publique el arreglo, si quieres.

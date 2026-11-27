# PiCameras: investigación inicial

> Fecha: 27-nov-2026 · Estado: investigación, sin diseño aprobado todavía.

## 1. Objetivo del proyecto

Un programa **open source** para ver y gestionar las cámaras de una red local:

- **Seguro por defecto:** solo en la red local, con autenticación y sin depender de nubes de terceros.
- **Cómodo:** que lo instale y configure cualquiera, sin saber qué es RTSP, Docker ni YAML.
- **Multimarca y multiprotocolo:** que acepte modelos y protocolos distintos.
- **Ligero:** que funcione en cualquier PC (Windows, macOS, Linux) y en una Raspberry Pi.
- **Cámaras Wi-Fi de consumo:** el problema de partida es que estas cámaras traen *bloatware* (app y nube del
  fabricante) y no se puede llegar a ellas de forma cómoda.

Primer modelo de referencia: **YI Dome Guard YRS.3019**, que tenemos aquí.

## 2. Lo que ya existe

### 2.1 Motores de streaming

**[go2rtc](https://github.com/AlexxIT/go2rtc)** (MIT, en Go, un único binario):

- Corre en Windows, macOS, Linux y ARM (Raspberry Pi).
- Fuentes que acepta: RTSP, RTMP, HTTP/MJPEG, ONVIF, HomeKit, FFmpeg y los protocolos propietarios de
  **Tuya, Xiaomi (CS2 y TUTK), Wyze (TUTK/DTLS), Tapo y Ring**, entre otros.
- Salidas: WebRTC (latencia baja en el navegador), MSE, HLS, MJPEG y RTSP para otros programas.
- La v1.9.14 (19-ene-2026) mejoró la fuente Xiaomi y le añadió TUTK
  ([release](https://github.com/AlexxIT/go2rtc/releases/tag/v1.9.14)).
- ONVIF: el audio bidireccional solo funciona con cámaras de Profile T.
- La integran Frigate y Home Assistant, así que está muy probado.

**Conclusión:** la capa de vídeo y protocolos **no hay que reinventarla**. go2rtc puede ser el motor que lleve
PiCameras dentro.

### 2.2 NVR y paneles completos

| Proyecto | Qué hace bien | Por qué no resuelve nuestro problema |
|---|---|---|
| **Frigate** | NVR con detección de objetos e integración con Home Assistant | Pide Docker y configuración en YAML. La mejor precisión depende de un servicio de pago (Frigate+) |
| **Agent DVR** (iSpy) | Multiplataforma, también en Raspberry Pi | Interfaz densa y pensada para usuarios técnicos |
| **Viseron** | NVR en Python que detecta por CPU, sin Coral | Configuración técnica |
| **Scrypted** | Puente entre ecosistemas (HomeKit, Google, etc.) | No recomienda la Pi 4 para NVR. Curva de aprendizaje alta |

Fuentes: [Agent DVR vs Frigate (iSpyConnect, parte interesada)](https://www.ispyconnect.com/agent-dvr-vs-frigate),
[software RTSP para Raspberry Pi](https://www.smartrtsp.com/software/rtsp-software-for-raspberry-pi),
[resumen de videovigilancia autoalojada](https://dev.to/selfhostingsh/video-surveillance-58b7).

**El hueco que vemos:** todos son potentes, pero ninguno es «enchufar y listo» para alguien no técnico.
PiCameras debería aportar justo eso: descubrir solo las cámaras, guiar al usuario con un asistente y ser seguro
sin que nadie tenga que configurarlo.

## 3. La cámara de referencia: YI Dome Guard YRS.3019

### 3.1 Cómo viene de serie

- Funciona con la app **YI Home** y la nube de YI (Kami). De serie **no ofrece RTSP ni ONVIF**.
- El vídeo remoto va por P2P **TUTK / Kalay** (ThroughTek), el mismo protocolo que usan Wyze y otras marcas.

### 3.2 Camino A: firmware alternativo yi-hack-Allwinner-v2

[roleoroleo/yi-hack-Allwinner-v2](https://github.com/roleoroleo/yi-hack-Allwinner-v2) (MIT, ~1,2k estrellas).

**Lo que aporta:**

- RTSP: `rtsp://IP/ch0_0.h264` (alta), `ch0_1.h264` (baja) y `ch0_2.h264` (solo audio). Hay que activarlo en
  la web del hack.
- **Servidor ONVIF** con stream, snapshot, **PTZ**, presets, eventos y WS-Discovery.
- Snapshot por CGI (`/cgi-bin/snapshot.sh`), eventos MQTT (movimiento, llanto) con TLS.
- Interfaz web, SSH, telnet y FTP. Autenticación en HTTP, RTSP y ONVIF.
- **Opción para cortar toda la nube.**

**Instalación:** se formatea una microSD en FAT32 y se copian `Factory/`, `yi-hack/` y `lower_half_init.sh`.
Si quieres, configuras el Wi-Fi en `Factory/configure_wifi.cfg`. Después se mete la tarjeta, se reinicia y se
entra en `http://IP-CAM`. La tarjeta se queda puesta: **si la quitas, la cámara vuelve a la de fábrica**, así que
es reversible. El propio proceso hace una copia de seguridad.

**Filas de la tabla de compatibilidad para la Dome Guard** (prefijo del número de serie / firmware / build):

| Prefijo SN | Firmware | Build | Nota |
|---|---|---|---|
| YRS | 9.0.05* | r30gb | **beta** (issue #484) |
| YRS | 9.0.46* | r35gb | estable |
| YRS | 12.1.47* | r35gb o r37gb | issue #1156 |
| QFUS | 9.0.46* / 12.1.47* | r35gb | estable |
| RFUS | 12.1.47* | r35gb o r37gb | issue #1156 |

Regla del proyecto: el prefijo del número de serie **y** el firmware tienen que coincidir con la misma fila.
Nunca se fuerza un build cambiándole el nombre a los ficheros.

**Problemas conocidos con la YRS.3019 en 2026:**

- [#1117](https://github.com/roleoroleo/yi-hack-Allwinner-v2/issues/1117) (mar-2026, r30gb sobre 9.0.05.32):
  sin IP por Wi-Fi y sin RTSP. El usuario acabó borrando la partición de configuración y hubo que recuperarla.
  Se cerró en octubre como *not planned* sin una solución.
- [#1167](https://github.com/roleoroleo/yi-hack-Allwinner-v2/issues/1167) (oct-2026, número de serie IFUS,
  9.0.05.33, builds r30gb y r35gb 0.3.7): el Wi-Fi, la web, SSH y FTP funcionan, pero **no hay vídeo**. El
  servidor RTSP nunca abre el puerto, porque `rmm` aborta con `alsaOpenPcm: audio open error: Resource busy`.
  Sigue abierta y sin solución.
- [#909](https://github.com/roleoroleo/yi-hack-Allwinner-v2/issues/909), con firmware v12: hubo quien lo arregló
  poniendo `r35gb` en `/yi-hack/model_suffix`.

**Lectura:** con firmware **9.0.46 o 12.1.47**, el camino A es razonable. Con **9.0.05**, es lotería.

### 3.3 Camino B: hablar el protocolo de serie (TUTK)

- La app de Homey [YI Camera Local](https://community.homey.app/t/app-pro-yi-camera-local-live-video-audio-and-pan-tilt/160090.md)
  (sep-2026) consigue vídeo local de una YI Dome 1080p **sin flashear**. Necesita la cuenta de YI: el inicio de
  sesión y la renovación de la sesión pasan por la nube, y el vídeo va por la red local. No publica el código ni
  el protocolo, y no menciona la Dome Guard.
- Hay precedentes abiertos con TUTK: [docker-wyze-bridge](https://github.com/mrlt8/docker-wyze-bridge) para Wyze
  y la fuente Xiaomi/TUTK de go2rtc. También una reimplementación para VTech, que tuvo que sacar una licencia TUTK
  con un móvil rooteado.
- **Pegas:** depende de la nube de YI en cada sesión, pide ingeniería inversa y se rompe con cada cambio del
  fabricante.

**Lectura:** es un experimento para más adelante, no la base del proyecto.

### 3.4 Datos que faltan de nuestra cámara

- [ ] **Versión de firmware** (en YI Home: Ajustes → Información de la cámara).
- [ ] **Prefijo del número de serie** (YRS…, IFUS…, QFUS…).
- [ ] **IP en la red local.** En el escaneo del 27-nov (192.168.1.0/24, puertos 23, 80, 443, 554, 8000, 8080,
  8554, 34567, 6668, 8443 y 1935) no apareció nada que parezca una cámara. Encaja con que, de serie, no abre
  puertos de cámara. Puede que estuviera apagada o en otra red.

## 4. Dirección propuesta (pendiente de aprobar)

1. **go2rtc como motor** de vídeo y protocolos, empaquetado dentro de PiCameras.
2. **Lo nuestro, encima:**
   - **Descubrimiento:** WS-Discovery/ONVIF, mDNS, barrido de puertos RTSP y huella por fabricante (OUI de la
     MAC y respuestas HTTP).
   - **Asistente de alta:** identifica la cámara, pide lo mínimo y prueba la conexión.
   - **Guías por modelo:** por ejemplo, «tu YI necesita la microSD del yi-hack: así se prepara».
   - **Seguridad por defecto:** escucha solo en la LAN, contraseña obligatoria al primer arranque, HTTPS local
     y sin puertos abiertos a internet. Para el acceso remoto, documentar Tailscale o WireGuard en lugar de abrir
     puertos.
   - **Interfaz web sencilla:** mosaico de cámaras, vista individual, PTZ y snapshots.
3. **Para la YI Dome Guard:** el camino A si su firmware lo permite. El camino B queda en investigación.

## 5. Decisiones abiertas

- Lenguaje y forma de distribución: binario único, Docker o instalador por plataforma.
- Alcance de la v1: ¿solo ver en directo o también grabar (NVR) y detectar movimiento?
- Licencia: alinearla con go2rtc (MIT) o elegir GPL/AGPL.
- Nombre definitivo: «PiCameras» es provisional.

## Fuentes

- go2rtc: https://github.com/AlexxIT/go2rtc · release v1.9.14: https://github.com/AlexxIT/go2rtc/releases/tag/v1.9.14
- yi-hack-Allwinner-v2: https://github.com/roleoroleo/yi-hack-Allwinner-v2
- Issues: [#484](https://github.com/roleoroleo/yi-hack-Allwinner-v2/issues/484),
  [#671](https://github.com/roleoroleo/yi-hack-Allwinner-v2/discussions/671),
  [#745](https://github.com/roleoroleo/yi-hack-Allwinner-v2/issues/745),
  [#909](https://github.com/roleoroleo/yi-hack-Allwinner-v2/issues/909),
  [#1117](https://github.com/roleoroleo/yi-hack-Allwinner-v2/issues/1117),
  [#1167](https://github.com/roleoroleo/yi-hack-Allwinner-v2/issues/1167)
- Homey YI Camera Local: https://community.homey.app/t/app-pro-yi-camera-local-live-video-audio-and-pan-tilt/160090
- docker-wyze-bridge: https://github.com/mrlt8/docker-wyze-bridge
- Comparativas de NVR: https://www.ispyconnect.com/agent-dvr-vs-frigate ·
  https://www.smartrtsp.com/software/rtsp-software-for-raspberry-pi ·
  https://dev.to/selfhostingsh/video-surveillance-58b7

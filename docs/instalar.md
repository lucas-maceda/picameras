# Instalar PiCameras

PiCameras son dos programas: `picameras` (la web, la seguridad y la búsqueda de cámaras) y `go2rtc` (el motor
de vídeo, que va encerrado y sin puertos propios). Todavía no hay versiones publicadas, así que `picameras` se
compila desde el código. Hace falta Go 1.26 o posterior, **en tu PC**: la Raspberry Pi no necesita Go.

## Raspberry Pi

Vale cualquier Pi desde la Zero 2 W. Como go2rtc no recodifica el vídeo, la CPU apenas trabaja: lo que cuenta
es la red. Para más de 6–8 cámaras a la vez, mejor una Pi 4 con cable de red.

**1. En tu PC, compila:**

```sh
scripts/compilar.sh
```

Elige el binario según lo que devuelva `uname -m` en la Pi:

| `uname -m` | Binario |
|---|---|
| `aarch64` (Raspberry Pi OS de 64 bits: Pi 3, 4, 5 y Zero 2 W) | `dist/picameras_linux_arm64` |
| `armv7l` (Raspberry Pi OS de 32 bits en Pi 2, 3 y 4) | `dist/picameras_linux_armv7` |
| `armv6l` (Pi 1 y Zero W) | `dist/picameras_linux_armv6` |

**2. Copia a la Pi** ese binario, `scripts/descargar-go2rtc.sh` y `deploy/picameras.service`:

```sh
scp dist/picameras_linux_arm64 scripts/descargar-go2rtc.sh deploy/picameras.service pi@raspberrypi.local:
```

**3. En la Pi, instálalo como servicio** (con un usuario propio sin privilegios):

```sh
sudo useradd --system --home-dir /var/lib/picameras --shell /usr/sbin/nologin picameras
sudo install -d -m 755 /opt/picameras
sudo install -m 755 picameras_linux_arm64 /opt/picameras/picameras
sudo sh descargar-go2rtc.sh /opt/picameras        # comprueba la SHA-256 de go2rtc
sudo install -m 644 picameras.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now picameras
```

**4. Mira la dirección, la huella del certificado y el código de configuración:**

```sh
sudo journalctl -u picameras -n 30 --no-pager
```

Abre la dirección desde el navegador de cualquier equipo de la red (`https://<ip-de-la-pi>:8443`), comprueba la
huella del certificado y crea tu usuario con el código.

## Windows

```powershell
go build -o picameras.exe ./cmd/picameras
powershell -ExecutionPolicy Bypass -File scripts\descargar-go2rtc.ps1
.\picameras.exe
```

La primera vez, Windows pregunta si permite el acceso a la red: acepta **solo en redes privadas**. Los datos
se guardan en `%AppData%\picameras`.

## macOS y Linux de escritorio

```sh
go build -o picameras ./cmd/picameras
sh scripts/descargar-go2rtc.sh .
./picameras
```

Los datos van a `~/Library/Application Support/picameras` (macOS) o `~/.config/picameras` (Linux).

## Opciones

```
picameras [-data DIR] [-listen :8443] [-go2rtc RUTA]
picameras reset-password [-data DIR]     # con el servicio parado: borra el usuario y genera otro código
picameras version
```

## Olvidé la contraseña

La contraseña no se recupera por red. Hace falta acceso al equipo:

```sh
sudo systemctl stop picameras
sudo -u picameras /opt/picameras/picameras reset-password -data /var/lib/picameras
sudo systemctl start picameras
sudo journalctl -u picameras -n 30 --no-pager   # código nuevo
```

Las cámaras se conservan.

## Cámaras que no aparecen

- **Cámaras Wi-Fi de marca** (YI, Xiaomi, Tuya, Wyze…): de serie no abren RTSP ni ONVIF. Algunas admiten un
  firmware alternativo. Para la YI Dome Guard, mira la [investigación inicial](investigacion/2026-11-27-picameras-investigacion-inicial.md).
- **ONVIF en otra red:** el multicast no cruza VPNs; usa *Otra red* o añádela a mano.
- **Puertos raros:** el barrido mira 554, 8554, 80 y 8080. Si tu cámara usa otro, añádela a mano.

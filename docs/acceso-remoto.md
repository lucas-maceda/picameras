# Cámaras en varios sitios

**Sí se pueden unir en un solo PiCameras**, y sin abrir ningún puerto a internet. La idea es crear una red
privada cifrada (una VPN) entre los sitios. Para PiCameras, las cámaras del otro sitio pasan a ser «otra red local»
más: las busca, las da de alta y las ve igual que las de casa.

Lo que **nunca** hay que hacer es abrir puertos en el router para ver una cámara desde fuera: es justo como se
cuelan en las cámaras de medio mundo. PiCameras, además, rechaza cualquier conexión que venga de una IP pública.

## Opción recomendada: Tailscale con un «router de subred» en cada sitio

[Tailscale](https://tailscale.com) monta la VPN sin tocar el router. Es gratis para uso personal y usa
WireGuard por debajo.

```
   SITIO A                                SITIO B
   ┌──────────────┐    túnel cifrado      ┌────────────────────┐
   │ PiCameras    │◄────────────────────► │ Pi Zero 2 W        │
   │ (+Tailscale) │     (Tailscale)       │ (Tailscale, router │
   └──────┬───────┘                       │  de subred)        │
          │ red de casa                   └─────────┬──────────┘
     cámaras del sitio A                      red del sitio B
                                              cámaras del sitio B
```

**En cada sitio remoto** hace falta un aparato encendido siempre dentro de esa red. Sirve cualquier Raspberry
Pi, también la más barata (una Pi Zero 2 W ronda los 20 €), o un router que ya traiga Tailscale (GL.iNet,
OpenWrt). En él:

```sh
curl -fsSL https://tailscale.com/install.sh | sh
sudo sh -c 'echo net.ipv4.ip_forward=1 > /etc/sysctl.d/99-tailscale.conf' && sudo sysctl --system
sudo tailscale up --advertise-routes=192.168.99.0/24   # la red de ese sitio
```

En el panel de Tailscale se aprueba esa ruta.

**En el equipo de PiCameras** (el de casa):

```sh
sudo tailscale up --accept-routes
```

**En PiCameras:** *Añadir cámara → Otra red*, escribe `192.168.99.0/24` y pulsa *Buscar ahí*. El multicast de
ONVIF no cruza la VPN, pero el barrido de puertos sí, así que encuentra las cámaras RTSP. Si alguna no sale,
añádela a mano con su IP.

**Y desde el móvil, fuera de casa:** instala Tailscale en el móvil y abre `https://<IP Tailscale del equipo de
PiCameras>:8443`. PiCameras acepta el rango de Tailscale (100.64.0.0/10) como red privada.

### Requisito: que cada sitio tenga su propia red

Si los routers de los dos sitios usan el mismo rango (por ejemplo, los dos `192.168.1.x`, el más habitual), las direcciones chocan. Lo más fácil es cambiar en
el router de uno de los sitios el rango de la red local (por ejemplo a `192.168.99.x`). Si eso no se puede,
Tailscale tiene una solución para redes repetidas («4via6»), pero es más lío.

## Alternativas

| Opción | Cuándo | Pega |
|---|---|---|
| **WireGuard entre routers** | Si los routers lo traen (Fritz!Box, GL.iNet, OpenWrt) | Configuración más técnica. No hace falta otro aparato |
| **Un PiCameras en cada sitio** | Si no quieres juntar las redes | Un panel por sitio. Se entra a cada uno por Tailscale |

## Ancho de banda

- El vídeo solo viaja **mientras alguien lo está mirando**: con nadie mirando, no se gasta nada.
- En el mosaico, PiCameras usa la **calidad baja** de cada cámara si se la has configurado («Ruta calidad
  baja», o automática en las YI con yi-hack): unos 0,3–1 Mbps por cámara. La vista grande usa la calidad alta
  (2–4 Mbps en 1080p).
- Lo que limita es la **subida** de la conexión del sitio remoto. Con fibra no hay problema. Con 4G o ADSL en
  el sitio remoto, configura siempre la ruta de calidad baja.

## Lo que decides tú

Abrir una cuenta de Tailscale y comprar un aparato para cada sitio son decisiones tuyas. El código de PiCameras
no depende de ninguna de las dos: funciona con cualquier VPN que una las redes.

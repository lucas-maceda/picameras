# PiCameras: instrucciones para Claude

Proyecto **open source y público**. Antes de escribir código, lee [`SECURITY.md`](SECURITY.md).

## Premisa que manda sobre todo lo demás

Alguien con el código completo delante **no puede poder entrar en las cámaras** de quien use PiCameras. Toda la
seguridad descansa en secretos generados en cada instalación, nunca en que el código sea secreto.

Al trabajar aquí:

- Nunca escribas en el repo contraseñas, claves, tokens, certificados privados, IPs o números de serie reales,
  ni credenciales «por defecto» que funcionen. Tampoco en tests, ejemplos ni documentación: usa marcadores
  (`<contraseña>`, `192.0.2.10`) o secretos generados en el momento.
- Nunca añadas rutas, cuentas o modos que se salten la autenticación (depuración, soporte, recuperación remota).
- go2rtc escucha solo en `127.0.0.1`; todo lo que llega de fuera pasa por la autenticación de PiCameras.
- Si una tarea choca con las reglas de `SECURITY.md`, no la resuelvas «de paso»: dilo y propón la alternativa
  segura. Cambiar una regla exige editar `SECURITY.md` con el porqué en la misma PR.
- Antes de cada commit, revisa el diff buscando secretos y datos de la red o las cámaras de Lucas.
- **Nada mínimamente trazable de Lucas sube al repo:** ni claves de API ni tokens, ni IPs (de su red, de sus
  cámaras, de Tailscale ni la pública), ni MACs, ni números de serie, ni nombres de sus equipos, ni sus sitios o
  localizaciones (ni siquiera cuántos sitios tiene o de qué tipo), ni modelos o nombres que devuelvan sus
  cámaras, ni el código de configuración, la huella de su certificado o algo de `%AppData%\picameras`. Lo que
  descubras de su red se lo dices en el chat; al repo solo van ejemplos inventados.
- Ejemplos de IP, solo de estos rangos: `192.0.2.x`, `198.51.100.x`, `203.0.113.x`, `2001:db8::` o las redes
  privadas de ejemplo `192.168.99.x`, `10.99.x.x`, `172.16.99.x` y `100.100.100.x`. MACs, ninguna.
- Lo frena el hook `.githooks/pre-commit` (actívalo con `git config core.hooksPath .githooks` si no lo está;
  `.githooks/pre-commit --todo` revisa el repo entero). La lista privada de Lucas vive en
  `.git/info/datos-privados`, que no se versiona: si aparece un dato nuevo suyo (otra IP, otra MAC, el nombre de
  un sitio), añádelo ahí. Nunca saltes el hook con `--no-verify`.
- Su correo como autor de los commits no le importa: no hace falta el noreply.

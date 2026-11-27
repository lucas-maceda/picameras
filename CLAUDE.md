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
- **Nunca suben al repo claves de API, tokens, ni datos de Lucas** (sus IPs, correos, números de serie, el código
  de configuración, la huella de su certificado ni nada de `%AppData%\picameras`). Lo frena el hook
  `.githooks/pre-commit` (actívalo con `git config core.hooksPath .githooks` si no lo está). Su lista de datos
  privados vive en `.git/info/datos-privados`, que no se versiona: si aparece un dato nuevo de Lucas (otra IP,
  otra cámara), añádelo ahí. Nunca saltes el hook con `--no-verify`.
- Los commits se firman con el correo noreply de GitHub (`<id>+lucas-maceda@users.noreply.github.com`), nunca con
  un correo personal: el autor de cada commit es público.

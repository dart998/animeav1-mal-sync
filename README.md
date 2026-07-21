# AnimeAV1 → MyAnimeList Sync


## Cambios de v1.4.0

- Matcher de seguridad reescrito: el número de temporada nunca puede rescatar un título base distinto.
- Búsqueda en MAL por título base y evaluación local de todos los candidatos.
- Coincidencias exactas, coincidencias de título base y coincidencias difusas se validan por separado.
- Las coincidencias difusas exigen similitud alta y solapamiento real de palabras.
- Rechazo de secuelas explícitas contra entradas sin temporada.
- Detección de temporadas expresadas como `2nd Season`, `Season 2`, números romanos o un número final.
- Invalidación automática de coincidencias inseguras guardadas por versiones anteriores.
- Umbrales predeterminados más conservadores: `BASE_TITLE_MATCH_THRESHOLD=88` y `TITLE_MATCH_THRESHOLD=88`.

Mantén `DRY_RUN=true` durante la primera ejecución y revisa el listado completo antes de activar escrituras.

Aplicación web en Go que lee la biblioteca de AnimeAV1 y sincroniza el progreso con MyAnimeList. AnimeAV1 se utiliza siempre como origen de solo lectura: la aplicación nunca escribe ni modifica datos allí.

La imagen está preparada para `linux/arm/v7` y se puede ejecutar en un WD My Cloud EX4100 mediante Docker y Portainer Community Edition.

## Cambios de la versión 1.4.0

Esta versión corrige los fallos detectados durante la primera sincronización con caché:

- Limita y simplifica las consultas enviadas al buscador de MAL para evitar `HTTP 400: invalid q` con títulos largos o caracteres especiales.
- Añade hasta tres intentos para consultas GET que fallen por timeout, HTTP 429 o errores 5xx, con esperas progresivas de 1 y 2 segundos.
- Compara también el título inglés, japonés y los sinónimos proporcionados por MAL.
- Rechaza de forma obligatoria coincidencias con temporadas explícitamente diferentes, por ejemplo `2nd Season` frente a `4th Season`.
- Invalida automáticamente entradas antiguas de caché cuando la temporada de AnimeAV1 no coincide con la temporada almacenada de MAL.
- Mantiene el umbral de coincidencia configurable y la protección `ONLY_INCREASE`.

Después de actualizar desde la versión 1.2.0 es recomendable pulsar **Eliminar caché** una vez antes de la primera sincronización. Ejecuta primero una simulación con `DRY_RUN=true`, revisa el resultado y solo después activa la escritura real.

> La aplicación no puede revertir automáticamente modificaciones incorrectas que una versión anterior ya haya realizado sobre otra temporada en MAL. Revisa manualmente las entradas afectadas antes de continuar.

## Funciones principales

- Autorización de MyAnimeList mediante OAuth PKCE.
- Lectura de la biblioteca desde `https://animeav1.com/cuenta/listas`.
- Emparejamiento de títulos de AnimeAV1 con sus entradas correspondientes en MAL.
- Actualización de episodios y estado solamente cuando existen cambios.
- Sincronización manual o automática.
- Intervalo automático predeterminado de 60 minutos.
- Barra de progreso para sincronizaciones manuales.
- Botón para detener una sincronización manual, incluidas sus peticiones HTTP activas.
- Terminal integrada con los últimos logs y timestamp legible.
- Historial persistente en JSONL, aunque el botón JSONL no se muestra en la interfaz.
- Favicon local de AnimeAV1.

## Caché y rendimiento

Las coincidencias AnimeAV1 ↔ MAL y su último estado confirmado se guardan en:

```text
/data/cache.json
```

En cada ejecución la aplicación descarga una sola vez la biblioteca de AnimeAV1. Después, para cada anime:

1. Busca su entrada en la caché.
2. Si título, episodios y estado no cambiaron y la entrada todavía está dentro del periodo de validación, la omite sin consultar MAL.
3. Si cambió o debe revalidarse, consulta directamente el `mal_id` conocido.
4. Solo vuelve a buscar por título cuando el anime es nuevo, el título cambió o el ID almacenado dejó de ser válido.
5. Solo envía un `PUT` a MAL cuando el estado o los episodios realmente difieren.

La revalidación periódica contra MAL es de 24 horas de forma predeterminada. Esto detecta cambios realizados directamente en MAL sin repetir todas las búsquedas en cada sincronización horaria.

El botón **Eliminar caché** borra únicamente `/data/cache.json`. No elimina la cookie de AnimeAV1, OAuth, configuración ni historial. La caché no puede eliminarse mientras se ejecuta una sincronización.

## Archivos persistentes

- `/data/config/config.json`: configuración, cookie de AnimeAV1 y credenciales OAuth de MAL.
- `/data/cache.json`: coincidencias y último estado validado.
- `/data/history.jsonl`: historial de sincronizaciones.

No elimines el volumen persistente al actualizar el contenedor.

## Variables de entorno para Portainer

El repositorio incluye `.env.example`. Copia su contenido en las variables de entorno del stack o impórtalo desde Portainer después de sustituir los valores de ejemplo.

Variables principales:

| Variable | Valor recomendado | Descripción |
|---|---:|---|
| `MAL_CLIENT_ID` | obligatorio | Client ID de la aplicación creada en MAL. |
| `MAL_CLIENT_SECRET` | según la aplicación | Client secret de MAL. |
| `MAL_REDIRECT_URI` | `http://IP_DEL_EX4100:8787/oauth/callback` | Debe coincidir exactamente con el registrado en MAL. |
| `SYNC_INTERVAL_MINUTES` | `60` | Intervalo de sincronización automática. |
| `AUTO_SYNC` | `false` | Activa o desactiva la sincronización automática. |
| `DRY_RUN` | `true` inicialmente | Simula las actualizaciones sin escribir en MAL. |
| `ONLY_INCREASE` | `true` | Impide reducir episodios vistos. |
| `TITLE_MATCH_THRESHOLD` | `80` | Puntuación mínima para aceptar una coincidencia. |
| `CACHE_REVALIDATE_HOURS` | `24` | Tiempo máximo antes de comprobar de nuevo una entrada en MAL. |
| `LOG_TIMEZONE` | `Europe/Madrid` | Zona horaria mostrada en los logs. |
| `DATA_DIR` | `/data` | Directorio persistente. |
| `LISTEN_ADDR` | `:8787` | Dirección y puerto del servidor web. |

La cookie de AnimeAV1 se introduce y verifica desde la interfaz web; no es necesario guardarla en el archivo de entorno.

## Despliegue con Portainer

Usa siempre una etiqueta fija en producción:

```yaml
services:
  animeav1-mal-sync:
    image: ovelayos/animeav1-mal-sync:v1.4.0
    container_name: animeav1-mal-sync
    restart: unless-stopped
    ports:
      - "8787:8787"
    env_file:
      - stack.env
    volumes:
      - animeav1-mal-data:/data
    security_opt:
      - seccomp=unconfined

volumes:
  animeav1-mal-data:
```

En el editor web de Portainer también puedes mantener las variables dentro de `environment:`. La opción `env_file` solo debe usarse cuando el archivo exista realmente en el host o el método de despliegue permita adjuntarlo.

Acceso:

```text
http://IP_DEL_EX4100:8787
```

## Publicar una nueva versión

El script `release.sh` automatiza GitHub y Docker Hub en una sola ejecución. Debe ejecutarse desde una copia clonada del repositorio que tenga configurado el remoto `origin` por SSH.

Primera preparación:

```bash
chmod +x release.sh
```

Publicación:

```bash
./release.sh
```

El script:

1. Pregunta la versión, por ejemplo `v1.4.0`.
2. Valida el formato y comprueba que la etiqueta todavía no exista.
3. Arranca `ssh-agent` cuando sea necesario y añade `~/.ssh/id_ed25519.pem` o la clave indicada.
4. Verifica el remoto y que la rama local incluya los últimos cambios de `origin/main`.
5. Actualiza `VERSION` y fija `docker-compose.portainer.yml` a la nueva etiqueta.
6. Ejecuta `git add -A`, por lo que incluye archivos nuevos, modificados y eliminados.
7. Muestra los cambios y pide confirmación.
8. Crea un commit con el nombre de la versión y una etiqueta Git anotada.
9. Sube la rama y la etiqueta a GitHub.
10. Construye una sola imagen ARMv7 y la publica con dos etiquetas:

```text
ovelayos/animeav1-mal-sync:v1.4.0
ovelayos/animeav1-mal-sync:latest
```

Las dos etiquetas apuntan al mismo digest. Las versiones anteriores conservan su etiqueta fija para permitir rollback.

### Por qué se usa `git add -A`

`git commit -am` solo incluye archivos que Git ya estaba siguiendo. No incorpora archivos nuevos como un favicon, un script o un archivo de configuración. `git add -A` evita que una versión se publique incompleta.

### Requisitos del script

- Git.
- Acceso SSH al repositorio de GitHub.
- Clave SSH válida; por defecto `~/.ssh/id_ed25519.pem`.
- Docker iniciado.
- Docker Buildx.
- Acceso a Docker Hub mediante contraseña o Access Token.

## Publicación manual alternativa

```bash
VERSION=v1.4.0

docker buildx build \
  --platform linux/arm/v7 \
  -t "ovelayos/animeav1-mal-sync:${VERSION}" \
  -t "ovelayos/animeav1-mal-sync:latest" \
  --push .
```

En Portainer utiliza la etiqueta fija de la versión y conserva siempre el volumen `animeav1-mal-data`.

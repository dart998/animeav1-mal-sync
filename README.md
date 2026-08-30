# AnimeAV1 → MyAnimeList Sync

Aplicación web en Go que lee la biblioteca de AnimeAV1 y sincroniza progreso y estado con MyAnimeList. En la rama estable, AnimeAV1 se usa como origen de solo lectura: la aplicación no modifica datos allí.

La imagen se publica para `linux/arm/v7`, `linux/arm64` y `linux/amd64`, con soporte específico para WD My Cloud EX4100 + Docker + Portainer CE.

## Estado actual

Versión estable: `1.7.3`.

Cambios recientes:

- Configuración de MyAnimeList desde la propia interfaz web.
- Las credenciales de aplicación de MAL se guardan en `/data/config/config.json`; ya no es necesario mantenerlas en el YAML de Portainer.
- Si la autorización MAL ya es válida, se ocultan Client ID, Client Secret, Redirect URI, Guardar configuración MAL y Conectar con MAL. La configuración queda accesible desde un desplegable para cambios manuales.
- Si la cookie de AnimeAV1 está verificada y es válida, se ocultan el textarea de cookie y sus botones. Se puede cambiar desde un desplegable.
- Los enlaces a AnimeAV1 se abren directamente mediante el `slug` real, sin reutilizar la cookie desde el backend para abrir la ficha.
- La caché permite asignación manual de uno o dos MAL ID para temporadas divididas.
- Publicación Docker y redeploy de Portainer automatizados con GitHub Actions + webhook.

## Funciones principales

- Autorización de MyAnimeList mediante OAuth PKCE.
- Lectura de la biblioteca desde AnimeAV1 mediante sesión HTTP.
- Emparejamiento de títulos AnimeAV1 ↔ MAL.
- Soporte para temporadas unificadas en AnimeAV1 y divididas en dos fichas MAL.
- Actualización de episodios y estado solo cuando existen cambios.
- Protección `ONLY_INCREASE` para impedir reducciones de episodios vistos.
- Sincronización manual o automática.
- Intervalo automático configurable.
- Barra de progreso y parada de sincronización manual.
- Caché persistente de coincidencias.
- Revalidación periódica contra MAL.
- Historial persistente de sincronizaciones.
- Herramientas para inspeccionar, recalcular o eliminar coincidencias de caché.
- Asignación manual de MAL ID cuando el matching automático no es seguro.

## Persistencia

El stack usa el volumen Docker con nombre fijo:

```text
animeav1-mal-sync-data
```

montado en:

```text
/data
```

Archivos principales:

- `/data/config/config.json`: configuración, cookie de AnimeAV1, configuración de aplicación MAL y tokens OAuth.
- `/data/cache.json`: coincidencias AnimeAV1 ↔ MAL y último estado validado.
- `/data/history.jsonl`: historial de sincronizaciones.

No elimines el volumen persistente al actualizar o recrear el contenedor.

## Configuración de AnimeAV1

La cookie de AnimeAV1 se introduce desde la interfaz web.

Cuando la sesión es válida, el formulario se oculta y solo se muestra el estado de la sesión. La opción para sustituir la cookie permanece disponible en un desplegable.

La aplicación verifica la cookie leyendo la biblioteca de AnimeAV1.

## Configuración de MyAnimeList

La configuración se realiza desde la interfaz web:

- Client ID.
- Client Secret, si la aplicación MAL lo utiliza.
- Redirect URI.

Los valores se guardan en `/data/config/config.json`.

Las variables `MAL_CLIENT_ID`, `MAL_CLIENT_SECRET` y `MAL_REDIRECT_URI` siguen siendo compatibles como fallback y para migraciones antiguas, pero no son necesarias en un despliegue nuevo.

Después de guardar la configuración, utiliza **Conectar con MAL** para completar OAuth PKCE. Si la sesión OAuth existente es válida, los campos de configuración y el botón de conexión se ocultan automáticamente.

## Variables de entorno

El stack actual utiliza:

| Variable | Predeterminado | Descripción |
|---|---:|---|
| `SYNC_INTERVAL_MINUTES` | `60` | Intervalo de sincronización automática. |
| `AUTO_SYNC` | `false` | Activa la sincronización automática. |
| `DRY_RUN` | `true` | Simula las actualizaciones sin escribir en MAL. |
| `ONLY_INCREASE` | `true` | Impide reducir episodios vistos. |
| `TITLE_MATCH_THRESHOLD` | `80` | Umbral mínimo del matching AnimeAV1 → MAL. |
| `REVERSE_TITLE_MATCH_THRESHOLD` | `92` | Reservado para la sincronización inversa. |
| `CACHE_REVALIDATE_HOURS` | `24` | Horas antes de revalidar una entrada contra MAL. |
| `LOG_TIMEZONE` | `Europe/Madrid` | Zona horaria de los logs. |
| `DATA_DIR` | `/data` | Directorio persistente. |
| `LISTEN_ADDR` | `:8787` | Dirección y puerto del servidor web. |

Para la primera ejecución se recomienda mantener `DRY_RUN=true`, revisar los resultados y desactivarlo después.

## Despliegue con Portainer desde Git

El stack está preparado para ser gestionado directamente desde este repositorio.

Configuración recomendada en Portainer:

```text
Repository URL:
https://github.com/dart998/animeav1-mal-sync

Repository reference:
refs/heads/main

Compose path:
docker-compose.yml
```

El compose utiliza siempre:

```yaml
image: ovelayos/animeav1-mal-sync:latest
```

Por tanto, un redeploy con repull obtiene la última versión estable publicada.

El acceso web predeterminado es:

```text
http://IP_DEL_EX4100:8787
```

## Publicación y despliegue automáticos

La publicación ya no depende de scripts locales.

El workflow `.github/workflows/docker-publish.yml` se ejecuta en cada `push` a `main` y también puede iniciarse manualmente desde GitHub Actions.

Flujo:

1. Checkout del repositorio.
2. Configuración de Go.
3. `go test ./...`.
4. Configuración de QEMU y Docker Buildx.
5. Login en Docker Hub mediante GitHub Secrets.
6. Lectura de la versión desde `VERSION`.
7. Build multiarch para:
   - `linux/arm/v7`
   - `linux/arm64`
   - `linux/amd64`
8. Publicación de:

```text
ovelayos/animeav1-mal-sync:<VERSION>
ovelayos/animeav1-mal-sync:latest
```

9. Si la publicación termina correctamente, GitHub Actions hace un `POST` al webhook de Portainer.
10. Portainer vuelve a desplegar el stack y hace pull de `latest`.

Secrets necesarios en GitHub Actions:

```text
DOCKERHUB_USERNAME
DOCKERHUB_TOKEN
PORTAINER_WEBHOOK_URL
```

`PORTAINER_WEBHOOK_URL` debe corresponder exclusivamente al stack `animeav1-mal-sync`.

## Versionado

La versión estable se guarda en el archivo:

```text
VERSION
```

El Dockerfile utiliza ese valor durante la compilación y GitHub Actions lo reutiliza como tag de Docker Hub.

Las versiones estables publican también `latest`. Las versiones de desarrollo/RC deben usar etiquetas explícitas y no sustituir `latest` hasta considerarse estables.

## Rollback

Cada versión estable conserva su tag específico en Docker Hub. Para volver temporalmente a una versión anterior, sustituye `latest` por el tag deseado en el compose y redepliega el stack, conservando siempre el mismo volumen `/data`.

## Caché y matching

Las coincidencias se almacenan en `/data/cache.json`.

En cada sincronización la aplicación reutiliza la caché cuando es segura y consulta MAL de nuevo cuando:

- el anime es nuevo;
- cambia el título;
- cambia el progreso o estado;
- vence el periodo de revalidación;
- el MAL ID guardado deja de ser válido.

El botón **Eliminar caché** elimina únicamente las coincidencias. No elimina configuración, cookie, OAuth ni historial.

Cuando el matching automático no alcanza el nivel de seguridad requerido, la interfaz permite indicar manualmente uno o dos MAL ID. La segunda entrada sirve para temporadas que AnimeAV1 agrupa y MAL divide en dos partes.

## Seguridad

- No se deben versionar cookies, tokens OAuth, contraseñas ni archivos reales de configuración.
- Los secretos de Docker Hub y Portainer se almacenan en GitHub Actions Secrets.
- El Client Secret de MAL no se devuelve rellenado al navegador una vez guardado.
- La persistencia sensible permanece dentro del volumen Docker en `/data/config/config.json`.

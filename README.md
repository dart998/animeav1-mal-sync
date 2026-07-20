# AnimeAV1 → MyAnimeList Sync v1.1.0

Aplicación estable para leer la biblioteca de AnimeAV1 y actualizar únicamente MyAnimeList.

AnimeAV1 se utiliza siempre como origen de solo lectura. La aplicación no llama a endpoints de escritura de AnimeAV1.

## Cambios de v1.1.0

- Barra de progreso durante la sincronización manual, con anime actual, elementos procesados, total y porcentaje.
- Caja de últimos logs integrada en la pantalla principal, con aspecto de terminal, texto blanco y fondo negro.
- Actualización automática del progreso cada segundo y de los logs cada dos segundos, sin recargar la página.
- Eliminado el botón `JSONL` de la interfaz. El endpoint `/history/raw` se conserva internamente para compatibilidad y diagnóstico.
- Intervalo predeterminado de sincronización automática cambiado a 60 minutos.
- Añadido el favicon oficial de AnimeAV1, servido localmente por la aplicación.

No se ha modificado el scraping de AnimeAV1, OAuth, matching ni la lógica de escritura en MAL.

## Construir y publicar con versión y latest

Desde PowerShell, dentro de la carpeta del proyecto:

```powershell
$VERSION = "v1.1.0"

docker buildx build `
  --platform linux/arm/v7 `
  -t "ovelayos/animeav1-mal-sync:$VERSION" `
  -t "ovelayos/animeav1-mal-sync:latest" `
  --push .
```

Las dos etiquetas se generan en la misma compilación y apuntan al mismo digest.

## Despliegue en Portainer

El proceso escucha dentro del contenedor en el puerto `8787`.

```text
Host 8787 → Contenedor 8787/TCP
```

La interfaz queda disponible en:

```text
http://IP_DEL_EX4100:8787
```

Usa en producción la etiqueta fija:

```text
ovelayos/animeav1-mal-sync:v1.1.0
```

No elimines el volumen persistente al actualizar. Se conservarán la cookie, los tokens de MAL, los ajustes y el historial.

## Variables necesarias

- `MAL_CLIENT_ID`
- `MAL_CLIENT_SECRET`, únicamente si la aplicación MAL lo requiere
- `MAL_REDIRECT_URI`, por ejemplo `http://IP_DEL_EX4100:8787/oauth/callback`

## Variables opcionales

- `ANIMEAV1_LIBRARY_URL`: `https://animeav1.com/cuenta/listas`
- `TITLE_MATCH_THRESHOLD`: `80`
- `SYNC_INTERVAL_MINUTES`: `60`
- `DRY_RUN`: `true`
- `ONLY_INCREASE`: `true`
- `AUTO_SYNC`: `false`
- `LOG_TIMEZONE`: `Europe/Madrid`
- `DATA_DIR`: `/data`
- `LISTEN_ADDR`: `:8787`

## Historial

La pantalla principal muestra las últimas 40 líneas con formato de terminal. La vista ampliada continúa disponible en:

```text
http://IP_DEL_EX4100:8787/history
```

El historial se presenta de más reciente a más antiguo y con timestamp local al principio.

## Primera sincronización real

1. Guarda y verifica la cookie completa de AnimeAV1.
2. Conecta MyAnimeList.
3. Ejecuta primero una sincronización con **Modo simulación** activado.
4. Revisa el progreso y los últimos logs.
5. Desactiva el modo simulación únicamente cuando los emparejamientos sean correctos.

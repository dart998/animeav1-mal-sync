# AnimeAV1 → MyAnimeList Sync v1.0.0

Versión estable de la aplicación que lee la biblioteca de AnimeAV1 y actualiza únicamente MyAnimeList.

AnimeAV1 se utiliza siempre como origen de solo lectura. La aplicación no llama a endpoints de escritura de AnimeAV1.

## Alcance de v1.0.0

Se ha mantenido intacto el flujo que ya funciona:

- lectura autenticada de `https://animeav1.com/cuenta/listas`;
- extracción de `libraryEntries` desde SvelteKit;
- OAuth PKCE con MyAnimeList;
- matching por títulos, alias y episodios;
- modo simulación;
- sincronización manual y automática;
- persistencia en `/data/config/config.json`;
- historial persistente en `/data/history.jsonl`.

Solo se han incorporado tres correcciones de cierre:

1. **Timestamp legible al principio de cada línea del historial.** El historial visual continúa mostrando primero la entrada más reciente y utiliza por defecto la zona `Europe/Madrid`.
2. **Estado de AnimeAV1 coherente.** Una sincronización correcta marca automáticamente la sesión como válida; un fallo muestra el motivo real en la pantalla principal.
3. **Historial JSONL original conservado.** `/history` es la vista legible y `/history/raw` conserva el formato original para exportación o diagnóstico.

No se ha cambiado la arquitectura, el matching ni la lógica de escritura en MAL para evitar introducir riesgo en una aplicación que ya funciona.

## Construir y publicar

Desde PowerShell, dentro de la carpeta del proyecto:

```powershell
docker buildx build --platform linux/arm/v7 -t ovelayos/animeav1-mal-sync:latest --push .
```

## Despliegue en Portainer

El proceso escucha dentro del contenedor en el puerto `8787`.

Mapeo recomendado:

```text
Host 8787 → Contenedor 8787/TCP
```

La interfaz quedará disponible en:

```text
http://IP_DEL_EX4100:8787
```

No elimines el volumen persistente al actualizar. Así se conservan la cookie, los tokens de MAL, los ajustes y el historial.

## Variables necesarias

- `MAL_CLIENT_ID`
- `MAL_CLIENT_SECRET`, únicamente si la aplicación MAL lo requiere
- `MAL_REDIRECT_URI`, por ejemplo `http://IP_DEL_EX4100:8787/oauth/callback`

## Variables opcionales

- `ANIMEAV1_LIBRARY_URL`: `https://animeav1.com/cuenta/listas`
- `TITLE_MATCH_THRESHOLD`: `80`
- `SYNC_INTERVAL_MINUTES`: `15`
- `DRY_RUN`: `true`
- `ONLY_INCREASE`: `true`
- `AUTO_SYNC`: `false`
- `LOG_TIMEZONE`: `Europe/Madrid`
- `DATA_DIR`: `/data`
- `LISTEN_ADDR`: `:8787`

## Historial

Vista legible, ordenada de más reciente a más antiguo:

```text
http://IP_DEL_EX4100:8787/history
```

Ejemplo:

```text
[2026-07-20 22:49:56 CEST] {"animeav1_media_id":257,"dry_run":true,...}
```

JSONL original, sin transformación:

```text
http://IP_DEL_EX4100:8787/history/raw
```

## Primera sincronización real

1. Guarda y verifica la cookie completa de AnimeAV1.
2. Conecta MyAnimeList.
3. Ejecuta primero una sincronización con **Modo simulación** activado.
4. Revisa `/history`.
5. Desactiva el modo simulación únicamente cuando los emparejamientos sean correctos.

## Estados AnimeAV1 utilizados

| AnimeAV1 | MyAnimeList |
|---:|---|
| 0 | watching |
| 1 | plan_to_watch |
| 2 | completed |
| 3 | on_hold |
| 4 | dropped |

# AnimeAV1 → MyAnimeList Sync v0.3

Aplicación única en Go para sincronizar la biblioteca de AnimeAV1 hacia MyAnimeList.

AnimeAV1 se utiliza exclusivamente como origen de datos. La aplicación nunca llama a endpoints de escritura de AnimeAV1.

## Novedades de v0.3

- Lectura autenticada por HTTP de `https://animeav1.com/cuenta/listas`.
- Extracción directa de `libraryEntries` desde los datos iniciales de SvelteKit.
- Sin Playwright, navegador, Node ni Python en producción.
- Lectura de título, títulos alternativos, progreso, estado, puntuación, favorito, slug y episodios totales.
- Matching con MAL mediante título, títulos alternativos y número de episodios.
- OAuth 2.0 PKCE de MyAnimeList y renovación automática del token.
- Modo simulación activado por defecto.
- Sincronización manual y automática configurable.
- Migración automática desde `/data/state.json`.
- Configuración persistente en `/data/config/config.json`.
- Historial en `/data/history.jsonl`.

## Construir y publicar

Desde PowerShell, dentro de la carpeta del proyecto:

```powershell
docker buildx build --platform linux/arm/v7 -t ovelayos/animeav1-mal-sync:latest --push .
```

## Stack de Portainer

Edita únicamente estas variables en `docker-compose.portainer.yml`:

- `MAL_CLIENT_ID`
- `MAL_CLIENT_SECRET`, solo si tu aplicación MAL lo requiere
- `MAL_REDIRECT_URI`

El callback debe coincidir exactamente con el registrado en MyAnimeList:

```text
http://IP_DEL_EX4100:8787/oauth/callback
```

Después despliega el stack y abre:

```text
http://IP_DEL_EX4100:8787
```

## Primera ejecución

1. Pega la cabecera Cookie completa de una sesión abierta en AnimeAV1.
2. Pulsa **Verificar**. Debe indicar cuántas entradas ha leído.
3. Conecta MyAnimeList.
4. Mantén activado **Modo simulación**.
5. Ejecuta una sincronización manual y revisa el historial.
6. Desactiva el modo simulación cuando los emparejamientos sean correctos.

## Estados AnimeAV1 utilizados

| AnimeAV1 | MAL |
|---:|---|
| 0 | watching |
| 1 | plan_to_watch |
| 2 | completed |
| 3 | on_hold |
| 4 | dropped |

## Variables opcionales

- `ANIMEAV1_LIBRARY_URL`: por defecto `https://animeav1.com/cuenta/listas`
- `TITLE_MATCH_THRESHOLD`: por defecto `80`
- `SYNC_INTERVAL_MINUTES`: por defecto `15`
- `DRY_RUN`: por defecto `true`
- `ONLY_INCREASE`: por defecto `true`
- `AUTO_SYNC`: por defecto `false`

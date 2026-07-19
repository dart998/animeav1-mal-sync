# AnimeAV1 → MyAnimeList Sync para WD EX4100

Proyecto completo para compilar en Windows 11/PowerShell mediante WSL o Git Bash y publicar una imagen Docker ARMv7 en Docker Hub.

## Publicar

Desde Git Bash o WSL, dentro de esta carpeta:

```bash
chmod +x publicar.sh
./publicar.sh
```

El script:

1. comprueba Docker y Buildx;
2. inicia sesión en Docker Hub;
3. compila para `linux/arm/v7`;
4. publica la imagen;
5. genera `docker-compose.portainer.generado.yml`.

## Desplegar en Portainer

1. Abre `docker-compose.portainer.generado.yml`.
2. Sustituye `TU_MAL_CLIENT_ID` y `TU_MAL_CLIENT_SECRET`.
3. En Portainer: **Stacks → Add stack → Web editor**.
4. Pega el contenido y despliega.
5. Abre `http://IP_DEL_EX4100:8787`.
6. Pega la cookie de AnimeAV1 desde la interfaz.
7. Conecta MyAnimeList desde el botón OAuth.

Callback de MAL recomendado:

```text
http://IP_DEL_EX4100:8787/oauth/callback
```

La primera prueba debe hacerse con `DRY_RUN=true`.

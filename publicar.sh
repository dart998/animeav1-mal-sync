#!/usr/bin/env bash
set -Eeuo pipefail

command -v docker >/dev/null 2>&1 || { echo "No encuentro Docker." >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo "Docker no está iniciado." >&2; exit 1; }
docker buildx version >/dev/null 2>&1 || { echo "Docker Buildx no está disponible." >&2; exit 1; }

read -r -p "Usuario de Docker Hub: " DOCKER_USER
read -r -p "Repositorio [animeav1-mal-sync]: " DOCKER_REPO
DOCKER_REPO=${DOCKER_REPO:-animeav1-mal-sync}
read -r -p "Etiqueta [armv7]: " IMAGE_TAG
IMAGE_TAG=${IMAGE_TAG:-armv7}

[[ -n "$DOCKER_USER" ]] || { echo "Falta el usuario de Docker Hub." >&2; exit 1; }
IMAGE="$DOCKER_USER/$DOCKER_REPO:$IMAGE_TAG"
BUILDER="animeav1-arm-builder"

cd "$(dirname "$0")"

echo
echo "Inicia sesión en Docker Hub. Usa un Personal Access Token como contraseña."
docker login -u "$DOCKER_USER"

if ! docker buildx inspect "$BUILDER" >/dev/null 2>&1; then
  docker buildx create --name "$BUILDER" --driver docker-container --use
else
  docker buildx use "$BUILDER"
fi

docker buildx inspect --bootstrap >/dev/null

echo
echo "Construyendo y publicando $IMAGE para linux/arm/v7..."
docker buildx build \
  --platform linux/arm/v7 \
  --tag "$IMAGE" \
  --push \
  .

sed \
  -e "s#TU_USUARIO_DOCKERHUB/animeav1-mal-sync:armv7#$IMAGE#g" \
  docker-compose.portainer.yml > docker-compose.portainer.generado.yml

echo
echo "Publicado: $IMAGE"
echo "Stack generado: docker-compose.portainer.generado.yml"

#!/usr/bin/env bash
set -Eeuo pipefail

PROJECT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
BRANCH="feature/mal-to-animeav1"
DOCKER_USER="ovelayos"
DOCKER_REPO="animeav1-mal-sync"
BUILDER="animeav1-arm-builder"

fail(){ echo "ERROR: $*" >&2; exit 1; }
command -v git >/dev/null 2>&1 || fail "Git no está instalado."
command -v docker >/dev/null 2>&1 || fail "Docker no está instalado."
docker info >/dev/null 2>&1 || fail "Docker no está iniciado."
docker buildx version >/dev/null 2>&1 || fail "Docker Buildx no está disponible."

cd "$PROJECT_DIR"
[ "$(git branch --show-current)" = "$BRANCH" ] || fail "Cambia primero a $BRANCH."
[ -z "$(git status --porcelain)" ] || fail "Hay cambios locales sin guardar."

git fetch origin "$BRANCH"
git pull --ff-only origin "$BRANCH"

VERSION="$(tr -d '[:space:]' < VERSION | sed 's/^v//')"
[[ "$VERSION" == *-* ]] || fail "release-rc.sh solo publica versiones prerelease (ej. 1.7.0-rc1)."
grep -Fq "appVersion = \"$VERSION\"" main.go || fail "main.go no coincide con VERSION=$VERSION."
IMAGE="${DOCKER_USER}/${DOCKER_REPO}:${VERSION}"

echo "Rama:  $BRANCH"
echo "Commit: $(git rev-parse --short HEAD)"
echo "Imagen: $IMAGE"
read -r -p "¿Construir y publicar RC ARMv7? [Y/n]: " CONFIRM
CONFIRM="${CONFIRM:-Y}"
[[ "$CONFIRM" =~ ^[Yy]$ ]] || exit 0

if ! docker buildx inspect "$BUILDER" >/dev/null 2>&1; then
  docker buildx create --name "$BUILDER" --driver docker-container --use
else
  docker buildx use "$BUILDER"
fi
docker buildx inspect --bootstrap >/dev/null

docker buildx build --platform linux/arm/v7 --tag "$IMAGE" --push .

echo "Publicado: $IMAGE"
echo "No se ha modificado ovelayos/animeav1-mal-sync:latest."
echo "Usa temporalmente esta imagen en el mismo stack de Portainer: $IMAGE"

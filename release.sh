#!/usr/bin/env bash
set -Eeuo pipefail

PROJECT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_DOCKER_USER="ovelayos"
DEFAULT_DOCKER_REPO="animeav1-mal-sync"
DEFAULT_SSH_KEY="${HOME}/.ssh/id_ed25519.pem"
DEFAULT_BRANCH="main"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

command -v git >/dev/null 2>&1 || fail "Git no está instalado."
command -v ssh-add >/dev/null 2>&1 || fail "ssh-add no está disponible."
command -v docker >/dev/null 2>&1 || fail "Docker no está instalado."
docker info >/dev/null 2>&1 || fail "Docker no está iniciado."
docker buildx version >/dev/null 2>&1 || fail "Docker Buildx no está disponible."

cd "$PROJECT_DIR"
[ -d .git ] || fail "$PROJECT_DIR no es un repositorio Git."

CURRENT_VERSION=""
if [ -f VERSION ]; then
  CURRENT_VERSION="v$(tr -d '[:space:]' < VERSION | sed 's/^v//')"
fi

read -r -p "Versión${CURRENT_VERSION:+ [$CURRENT_VERSION]} (ej. v1.4.0): " VERSION
VERSION="${VERSION:-$CURRENT_VERSION}"
VERSION="v${VERSION#v}"

[[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || \
  fail "Versión no válida. Usa un formato como v1.4.0."

read -r -p "Rama de Git [$DEFAULT_BRANCH]: " BRANCH
BRANCH="${BRANCH:-$DEFAULT_BRANCH}"

read -r -p "Clave SSH [$DEFAULT_SSH_KEY]: " SSH_KEY
SSH_KEY="${SSH_KEY:-$DEFAULT_SSH_KEY}"
[ -f "$SSH_KEY" ] || fail "No existe la clave SSH: $SSH_KEY"

read -r -p "Usuario de Docker Hub [$DEFAULT_DOCKER_USER]: " DOCKER_USER
DOCKER_USER="${DOCKER_USER:-$DEFAULT_DOCKER_USER}"

read -r -p "Repositorio de Docker Hub [$DEFAULT_DOCKER_REPO]: " DOCKER_REPO
DOCKER_REPO="${DOCKER_REPO:-$DEFAULT_DOCKER_REPO}"

VERSION_IMAGE="${DOCKER_USER}/${DOCKER_REPO}:${VERSION}"
LATEST_IMAGE="${DOCKER_USER}/${DOCKER_REPO}:latest"
BUILDER="animeav1-arm-builder"

# Arranca un agente SSH solo cuando esta sesión no tiene uno utilizable.
if ! ssh-add -l >/dev/null 2>&1; then
  eval "$(ssh-agent -s)" >/dev/null
fi
ssh-add "$SSH_KEY"

# Verifica que el remoto SSH funciona antes de modificar o publicar nada.
git remote get-url origin >/dev/null 2>&1 || fail "No existe el remoto Git 'origin'."
REMOTE_URL="$(git remote get-url origin)"
case "$REMOTE_URL" in
  git@*|ssh://*) ;;
  *) echo "AVISO: origin no parece usar SSH: $REMOTE_URL" ;;
esac

git fetch origin "$BRANCH"
CURRENT_BRANCH="$(git branch --show-current)"
[ "$CURRENT_BRANCH" = "$BRANCH" ] || fail "Estás en '$CURRENT_BRANCH'. Cambia a '$BRANCH' antes de publicar."

if ! git merge-base --is-ancestor "origin/$BRANCH" HEAD; then
  fail "La rama local no contiene los últimos cambios de origin/$BRANCH. Haz pull/rebase primero."
fi

if git rev-parse "$VERSION" >/dev/null 2>&1; then
  fail "La etiqueta Git $VERSION ya existe. Usa una versión nueva."
fi

# VERSION se guarda sin la v porque el código ya la añade al mostrarla.
printf '%s\n' "${VERSION#v}" > VERSION

# Mantiene el stack de Portainer fijado a esta versión concreta.
if [ -f docker-compose.portainer.yml ]; then
  sed -i.bak -E \
    "s#^([[:space:]]*image:)[[:space:]].*#\1 ${VERSION_IMAGE}#" \
    docker-compose.portainer.yml
  rm -f docker-compose.portainer.yml.bak
fi

git add -A

echo
echo "Cambios que se publicarán:"
git status --short

if git diff --cached --quiet; then
  fail "No hay cambios preparados. Actualiza el código o usa una versión distinta."
fi

echo
read -r -p "¿Publicar $VERSION en GitHub y Docker Hub? [y/N]: " CONFIRM
[[ "$CONFIRM" =~ ^[Yy]$ ]] || { echo "Cancelado."; exit 0; }

# El commit se crea antes del build para que la imagen publicada corresponda
# exactamente al código etiquetado en GitHub.
git commit -m "$VERSION"
git tag -a "$VERSION" -m "Release $VERSION"

echo
echo "Subiendo commit y etiqueta a GitHub..."
git push origin "$BRANCH"
git push origin "$VERSION"

echo
echo "Usando las credenciales guardadas por Docker Desktop para Docker Hub."
echo "Si el push falla por autenticación, ejecuta una sola vez: docker login -u $DOCKER_USER"

if ! docker buildx inspect "$BUILDER" >/dev/null 2>&1; then
  docker buildx create --name "$BUILDER" --driver docker-container --use
else
  docker buildx use "$BUILDER"
fi

docker buildx inspect --bootstrap >/dev/null

echo
echo "Construyendo y publicando para linux/arm/v7..."
docker buildx build \
  --platform linux/arm/v7 \
  --tag "$VERSION_IMAGE" \
  --tag "$LATEST_IMAGE" \
  --push \
  .

echo
echo "Publicación completada"
echo "  GitHub:     $BRANCH + etiqueta $VERSION"
echo "  Docker Hub: $VERSION_IMAGE"
echo "  Docker Hub: $LATEST_IMAGE"
echo
echo "En Portainer usa la etiqueta fija:"
echo "  $VERSION_IMAGE"

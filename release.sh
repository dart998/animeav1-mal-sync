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

read -r -p "Rama de Git [$DEFAULT_BRANCH]: " BRANCH
BRANCH="${BRANCH:-$DEFAULT_BRANCH}"

read -r -p "Clave SSH [$DEFAULT_SSH_KEY]: " SSH_KEY
SSH_KEY="${SSH_KEY:-$DEFAULT_SSH_KEY}"
[ -f "$SSH_KEY" ] || fail "No existe la clave SSH: $SSH_KEY"

read -r -p "Usuario de Docker Hub [$DEFAULT_DOCKER_USER]: " DOCKER_USER
DOCKER_USER="${DOCKER_USER:-$DEFAULT_DOCKER_USER}"

read -r -p "Repositorio de Docker Hub [$DEFAULT_DOCKER_REPO]: " DOCKER_REPO
DOCKER_REPO="${DOCKER_REPO:-$DEFAULT_DOCKER_REPO}"

if ! ssh-add -l >/dev/null 2>&1; then
  eval "$(ssh-agent -s)" >/dev/null
fi
ssh-add "$SSH_KEY"

git remote get-url origin >/dev/null 2>&1 || fail "No existe el remoto Git 'origin'."
REMOTE_URL="$(git remote get-url origin)"
case "$REMOTE_URL" in
  git@*|ssh://*) ;;
  *) echo "AVISO: origin no parece usar SSH: $REMOTE_URL" ;;
esac

CURRENT_BRANCH="$(git branch --show-current)"
[ "$CURRENT_BRANCH" = "$BRANCH" ] || fail "Estás en '$CURRENT_BRANCH'. Cambia a '$BRANCH' antes de publicar."

if [ -n "$(git status --porcelain)" ]; then
  fail "Hay cambios locales sin guardar. Este flujo publica únicamente commits de origin/$BRANCH."
fi

echo
echo "Descargando la última versión de origin/$BRANCH..."
git fetch --tags origin "$BRANCH"
git pull --ff-only origin "$BRANCH"

if [ -n "$(git status --porcelain)" ]; then
  fail "El repositorio dejó cambios locales después del pull. Revisa el estado antes de publicar."
fi

[ -f VERSION ] || fail "Falta el archivo VERSION."
REPO_VERSION="v$(tr -d '[:space:]' < VERSION | sed 's/^v//')"
[[ "$REPO_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || \
  fail "VERSION no contiene una versión válida: $REPO_VERSION"

read -r -p "Versión [$REPO_VERSION]: " VERSION
VERSION="${VERSION:-$REPO_VERSION}"
VERSION="v${VERSION#v}"
[ "$VERSION" = "$REPO_VERSION" ] || \
  fail "La versión solicitada ($VERSION) no coincide con la preparada en el repositorio ($REPO_VERSION)."

VERSION_IMAGE="${DOCKER_USER}/${DOCKER_REPO}:${VERSION#v}"
LATEST_IMAGE="${DOCKER_USER}/${DOCKER_REPO}:latest"
BUILDER="animeav1-arm-builder"
HEAD_COMMIT="$(git rev-parse HEAD)"

grep -Fq "appVersion = \"${VERSION#v}\"" main.go || \
  fail "main.go no contiene appVersion = \"${VERSION#v}\"."
grep -Fq "image: ${VERSION_IMAGE}" docker-compose.portainer.yml || \
  fail "docker-compose.portainer.yml no apunta a ${VERSION_IMAGE}."

echo
echo "Commit que se publicará:"
git --no-pager log -1 --oneline
echo "Imagen: $VERSION_IMAGE"
read -r -p "¿Crear/publicar la imagen Docker? [y/N]: " CONFIRM
[[ "$CONFIRM" =~ ^[Yy]$ ]] || { echo "Cancelado."; exit 0; }

if git rev-parse -q --verify "refs/tags/$VERSION" >/dev/null; then
  TAG_COMMIT="$(git rev-list -n 1 "$VERSION")"
  [ "$TAG_COMMIT" = "$HEAD_COMMIT" ] || \
    fail "La etiqueta $VERSION ya existe, pero apunta a otro commit ($TAG_COMMIT)."
  echo "La etiqueta $VERSION ya apunta al commit actual; se reutilizará."
else
  git tag -a "$VERSION" -m "Release $VERSION" "$HEAD_COMMIT"
  git push origin "$VERSION"
fi

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
echo "  GitHub:     $BRANCH @ $HEAD_COMMIT"
echo "  Etiqueta:   $VERSION"
echo "  Docker Hub: $VERSION_IMAGE"
echo "  Docker Hub: $LATEST_IMAGE"
echo
echo "En Portainer usa la etiqueta fija:"
echo "  $VERSION_IMAGE"

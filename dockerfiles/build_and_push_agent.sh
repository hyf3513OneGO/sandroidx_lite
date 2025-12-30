#!/usr/bin/env sh
set -euo pipefail

# 推送到 Docker Hub 的简单脚本
# 需要已通过 `docker login` 登陆 Docker Hub

# 必填：Docker Hub 用户名（例如：export DOCKERHUB_USERNAME=myname）
: "${DOCKERHUB_USERNAME:?请先通过环境变量 DOCKERHUB_USERNAME 指定 Docker Hub 用户名}"

# 可选：镜像名和 tag
: "${IMAGE_NAME:=agent-ubuntu-22.04}" # 仓库名（不含用户名）
: "${IMAGE_TAG:=latest}"              # tag

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
PROJECT_ROOT="$(dirname "${SCRIPT_DIR}")"
DOCKERFILE_PATH="${SCRIPT_DIR}/agent_ubuntu22.04.dockerfile"
CONTEXT_DIR="${PROJECT_ROOT}"

if [ ! -f "${DOCKERFILE_PATH}" ]; then
  echo "Dockerfile 不存在：${DOCKERFILE_PATH}" >&2
  exit 1
fi

IMAGE="${DOCKERHUB_USERNAME}/${IMAGE_NAME}:${IMAGE_TAG}"

echo "==> 构建镜像：${IMAGE}"
docker build \
  -f "${DOCKERFILE_PATH}" \
  -t "${IMAGE}" \
  "${CONTEXT_DIR}"

echo "==> 推送镜像：${IMAGE}"
docker push "${IMAGE}"

echo "完成，已推送到 Docker Hub：${IMAGE}"



FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive \
    TZ=Etc/UTC \
    PATH="/root/.local/bin:${PATH}"

# 基础工具
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      ca-certificates \
      curl \
      git \
      python3 \
      python3-pip \
      python3-venv \
      python3-distutils \
      bash \
      tini \
      android-tools-adb && \
    rm -rf /var/lib/apt/lists/*

# 确保 python / pip 命令可直接在 sh 中使用
RUN if ! command -v python >/dev/null 2>&1; then ln -s "$(command -v python3)" /usr/local/bin/python; fi && \
    if ! command -v pip >/dev/null 2>&1; then ln -s "$(command -v pip3)" /usr/local/bin/pip; fi

# 安装 uv（基于官方推荐安装脚本）
RUN curl -LsSf https://astral.sh/uv/install.sh | sh && \
    ln -s /root/.local/bin/uv /usr/local/bin/uv

WORKDIR /workspace

ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["sh", "-lc", "tail -f /dev/null"]



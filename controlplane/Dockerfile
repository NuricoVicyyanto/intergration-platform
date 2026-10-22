# syntax=docker/dockerfile:1.7

FROM node:22-bookworm-slim AS ui-builder

WORKDIR /build/ui
COPY controlplane/ui/package.json controlplane/ui/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY controlplane/ui/ ./
RUN npm run build


FROM golang:1.25-bookworm AS go-builder

WORKDIR /build/controlplane
ENV CGO_ENABLED=0 \
    GOOS=linux

COPY controlplane/go.mod controlplane/go.sum ./
RUN go mod download
COPY controlplane/ ./

RUN go build -trimpath -ldflags="-s -w" -o /out/control-plane ./cmd/control-plane && \
    go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker && \
    go build -trimpath -ldflags="-s -w" -o /out/provision ./cmd/provision


FROM python:3.13-slim AS runtime

ENV DEBIAN_FRONTEND=noninteractive \
    PYTHONUNBUFFERED=1 \
    PYTHONUTF8=1 \
    UV_LINK_MODE=copy \
    PATH="/opt/meltano/.venv/bin:${PATH}"

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      build-essential \
      ca-certificates \
      default-libmysqlclient-dev \
      freetds-bin \
      freetds-dev \
      git \
      libffi-dev \
      libkrb5-dev \
      libpq5 \
      libssl-dev \
      pkg-config \
      tdsodbc \
      unixodbc \
      unixodbc-dev && \
    pip install --no-cache-dir uv && \
    rm -rf /var/cache/apt/archives /var/lib/apt/lists/*

WORKDIR /opt/meltano
COPY pyproject.toml uv.lock README.md LICENSE ./
COPY src ./src

# Meltano dan driver PostgreSQL system database. Plugin extractor/loader
# di-install terpisah oleh layanan provision karena bergantung pada tenant.
RUN uv sync --frozen --no-dev --extra postgres --no-progress && \
    ln -s /opt/meltano/.venv/bin/meltano /usr/local/bin/meltano && \
    meltano --version

COPY --from=go-builder /out/control-plane /usr/local/bin/control-plane
COPY --from=go-builder /out/worker /usr/local/bin/worker
COPY --from=go-builder /out/provision /usr/local/bin/provision
COPY controlplane/docker/provision-once.sh /usr/local/bin/provision-once
RUN chmod 0755 /usr/local/bin/provision-once

WORKDIR /app
EXPOSE 8080
CMD ["/usr/local/bin/control-plane"]


FROM nginx:1.27-alpine AS ui

COPY controlplane/docker/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=ui-builder /build/ui/dist/ /usr/share/nginx/html/

EXPOSE 80

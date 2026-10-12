# Один Dockerfile на три процесса. Имя бинарника и порт задаёт сборка:
#
#   gateway  CMD=./gateway/cmd/gateway  PORT=8080  INSTALL_GIT=1
#   admin    CMD=./admin/cmd/admin      PORT=8081  INSTALL_GIT=0
#   api      CMD=./api/cmd/api          PORT=8082  INSTALL_GIT=0
#
# docker build -f deploy/Dockerfile --build-arg CMD=./gateway/cmd/gateway \
#   --build-arg PORT=8080 --build-arg INSTALL_GIT=1 -t gateway .
#
# Каталог /data общий: все три процесса открывают один DATA_FILE.
# git нужен шлюзу для worktree сессии. grok и claude в образ не входят.

FROM golang:1.22.12-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG CMD=./gateway/cmd/gateway
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/service "${CMD}"

FROM debian:bookworm-slim

ARG INSTALL_GIT=0
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && if [ "${INSTALL_GIT}" = "1" ]; then \
         apt-get install -y --no-install-recommends git \
         && git config --system --add safe.directory '*'; \
       fi \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --create-home --uid 65532 --user-group app \
    && mkdir -p /data \
    && chown app:app /data

COPY --from=build /out/service /usr/local/bin/service

ARG PORT=8080
ENV GATEWAY_ADDR=":${PORT}" \
    ADMIN_ADDR=":${PORT}" \
    API_ADDR=":${PORT}" \
    DATA_FILE=/data/gateway.json \
    WORKSPACES_DIR=/data/workspaces \
    ENV_FILE=/data/.env

USER app
WORKDIR /data
VOLUME /data
EXPOSE ${PORT}
ENTRYPOINT ["/usr/local/bin/service"]

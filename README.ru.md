<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="plugin/media/icon-dark.png">
    <img src="plugin/media/icon-light.png" width="128" alt="Remote LLM Gateway">
  </picture>
</p>

# Remote LLM Gateway

[![English](https://img.shields.io/badge/lang-EN-lightgrey)](README.md)
[![Русский](https://img.shields.io/badge/lang-RU-blue)](README.ru.md)
[![Лицензия: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![CI](https://github.com/AlanForester/remote-llm-gateway/actions/workflows/ci.yml/badge.svg)](https://github.com/AlanForester/remote-llm-gateway/actions/workflows/ci.yml)
[![Релиз](https://img.shields.io/github/v/release/AlanForester/remote-llm-gateway)](https://github.com/AlanForester/remote-llm-gateway/releases)
[![Open VSX](https://img.shields.io/open-vsx/v/alanforester/remote-llm-gateway?label=Open%20VSX)](https://open-vsx.org/extension/alanforester/remote-llm-gateway)
[![Звезда на GitHub](https://img.shields.io/github/stars/AlanForester/remote-llm-gateway?style=social)](https://github.com/AlanForester/remote-llm-gateway/stargazers)

Поддержите проект — поставьте [звезду на GitHub](https://github.com/AlanForester/remote-llm-gateway/stargazers).

> **Remote LLM Gateway** — один шлюз для Grok Build, Claude Code и GPT. Плагин в VS Code и Cursor и CLI `remote-llm` работают с локальным проектом, а агенты, инструменты и MCP выполняются на сервере. Проект не связан с xAI, Anthropic, OpenAI и Meta и не представляет их. Имена *Grok*, *Grok Build*, *Claude Code*, *Codex* и *Muse Code* используются только чтобы обозначить совместимость.

Три входа в один и тот же шлюз:

| | **Плагин** | **CLI `remote-llm`** | **Админка** |
|---|---|---|---|
| **Что это** | Чат в боковой панели VS Code и Cursor | Консольный клиент, по ощущениям как `grok` или `claude` | Пользователи, токены, модели, лимиты, расход |
| **Где лежит** | `plugin/` | `cmd/remote-llm/` | `admin/` |
| **Когда удобно** | Проект уже открыт в редакторе | Нужна консоль, редактор не нужен | Нужно назначить модель, прокси и доступ |

Плагин и CLI подключаются к шлюзу по WebSocket и говорят с ним на ACP (JSON-RPC). К API моделей они не ходят. Шлюз на сервере поднимает агента в отдельном каталоге этой сессии. Подписка одна, ключи лежат на шлюзе.

Регистрацию в Claude, например, и у остальных таких сервисов проходят на том же сервере, где стоит шлюз. Вход и работа идут с одного адреса.

---

## Зачем это

Тяжёлый CLI не стоит на каждой машине. Люди открывают свой проект локально, а Grok Build, Claude Code и GPT работают на одном сервере. Администратор видит расход, назначает человеку модель и выход в интернет и ставит лимит токенов на период.

### Возможности

<details>
<summary><strong>Один шлюз и одна подписка</strong></summary>

Все пользователи подключаются к одному шлюзу. Ключи провайдеров на их компьютеры не раздаются. Сессия собирается из пользователя и проекта: `wss://<адрес-шлюза>/ws/{sessionId}`.

</details>

<details>
<summary><strong>Плагин для VS Code и Cursor</strong></summary>

Плагин **Remote LLM Gateway** показывает чат в правой боковой панели, рядом с Grok. Проект открыт локально: в начале сессии файлы уходят на шлюз, после ответа правки записываются обратно в редактор. Локальный процесс `grok` или `claude` он не запускает.

Если в админке этому пользователю назначена LLM, плагин показывает её. Если назначения нет, модель выбирается в интерфейсе.

За основу интерфейса взят [GUI for Grok Build & Muse Code](https://github.com/phuryn/grok-build-vscode).

</details>

<details>
<summary><strong>CLI <code>remote-llm</code></strong></summary>

`remote-llm` смотрит в тот же шлюз, что и плагин. Из каталога проекта он отправляет файлы на сервер и после ответа записывает правки обратно. Флаг `--no-sync` этого не делает. Короткое имя `llm` не используется: оно слишком общее.

Принудительная модель из админки отображается и здесь.

</details>

<details>
<summary><strong>Админка</strong></summary>

В админке пользователи, токены доступа, доступные модели и расход: запросы и токены. Там же исходящий прокси, принудительная LLM и лимит использования: сколько токенов можно потратить и через какой период счётчик сбрасывается.

`api` пускает вызовы по пользовательским токенам. Сессии агентов принимает `gateway`.

</details>

<details>
<summary><strong>Принудительная модель</strong></summary>

Администратор назначает пользователю LLM. Плагин и CLI показывают эту модель на экране. Без назначения человек выбирает модель сам.

</details>

<details>
<summary><strong>Лимит токенов</strong></summary>

В админке у пользователя два поля: лимит токенов и период сброса. Период может быть сутками, неделей, месяцем или своим интервалом. Шлюз считает токены этого периода. После лимита новые запросы не проходят, пока период не закончится и счётчик не обнулится. Расход за прошлые периоды в аналитике остаётся.

</details>

<details>
<summary><strong>Исходящий прокси</strong></summary>

Запросы Grok Build, Claude Code и остальных агентов с сервера наружу идут через прокси. Адрес задаётся в админке и в `.env`. Смена любого из этих значений меняет выход, пересобирать сервис для этого не нужно.

</details>

<details>
<summary><strong>Отдельный каталог на сессию</strong></summary>

Файлы пользователя на шлюзе лежат в каталоге этой сессии. Путь проекта с клиента — только ключ каталога: шлюз не открывает по нему репозиторий на сервере. Команды агента выполняются в этом каталоге. Правки возвращаются в локальный проект.

</details>

<details>
<summary><strong>Несколько агентов</strong></summary>

| Агент | Как запускается |
|---|---|
| Grok Build | `grok agent serve`, ACP через WebSocket |
| Claude Code | `claude agent serve` |
| GPT | OpenAI Responses API и tool calling в самом шлюзе. Ключ один, на сервере |

У GPT шлюз сам даёт инструменты: прочитать, записать и показать файлы сессии, выполнить команду в её каталоге. Дополнительные инструменты берутся из stdio MCP, список серверов лежит в `MCP_CONFIG`. Grok Build и Claude Code по-прежнему берут свои MCP-серверы из настройки на машине шлюза.

</details>

---

## Требования

- VS Code или Cursor для плагина. Для консоли достаточно `remote-llm`.
- Сервер шлюза с установленными CLI тех агентов, которые вы включаете: `grok`, `claude`.
- Одна подписка или один ключ провайдера на шлюзе.
- Регистрация и вход в Claude и в остальные такие сервисы с того же сервера, где стоит шлюз.
- Redis, когда шлюз, админка и API работают отдельными процессами или на нескольких машинах. Локальный `go run` без Redis хранит пользователей, лимиты и прокси в `DATA_FILE`. Сессии WebSocket в Redis не пишутся: это живой разговор, он не переживает рестарт процесса.

---

## Установка

Релиз [v0.1.2](https://github.com/AlanForester/remote-llm-gateway/releases/tag/v0.1.2) содержит бинарники `remote-llm`, пакет `.vsix` и образы этой версии в GHCR. Плагин в Marketplace не опубликован. Локально сервисы запускаются из исходников.

```bash
cp .env.example .env
# задайте ADMIN_TOKEN в .env
set -a && source .env && set +a
go run ./gateway/cmd/gateway
go run ./admin/cmd/admin
go run ./api/cmd/api
```

Админка открывается на `http://127.0.0.1:8081`. Токен пользователя показывается один раз при создании. Дальше:

```bash
go run ./cmd/remote-llm --gateway http://127.0.0.1:8080 --token <токен>
```

`GATEWAY_ECHO=1` в `.env` отвечает эхом и не требует установленных `grok` и `claude`. Для настоящих агентов уберите эту переменную: шлюз запустит `grok agent serve` или `claude agent serve` и пошлёт GPT в OpenAI Responses API. Вход в Claude и остальные такие сервисы делается на машине шлюза.

Один внешний вход описан в [`deploy/nginx/nginx.conf`](deploy/nginx/nginx.conf). Nginx принимает обычные запросы и WebSocket, а процессы слушают localhost:

| Путь | Куда |
|---|---|
| `/`, `/login`, `/users`, `/proxy` | админка |
| `/v1/`, `/ws/` | шлюз, включая сессию ACP |
| `/api/` | API, префикс снаружи снимается |
| `/health`, `/health/admin`, `/health/api` | проверки трёх процессов |

Тогда `GATEWAY_URL` указывает на nginx, без порта `8080`. В `.env.example` есть закомментированный вариант.

Монорепозиторий:

- `gateway/` — шлюз, WebSocket и ACP
- `api/` — доступ по пользовательским токенам
- `admin/` — веб-админка
- `plugin/` — расширение VS Code и Cursor
- `cmd/remote-llm/` — CLI
- `deploy/` — выкладка

---

## Быстрый старт

1. Поднять шлюз, `api` и админку командами из раздела «Установка». Состояние лежит в `DATA_FILE`, Redis для этого шага не нужен.
2. В админке завести пользователя. При необходимости задать модель, лимит токенов, период сброса и прокси.
3. В плагине указать `remoteLlm.gatewayUrl` и командой «Remote LLM: задать токен» сохранить токен в Secret Storage. Чат открывается командой «Remote LLM: открыть чат» и появляется вкладкой в правой панели. Команда «Remote LLM: показать назначенную модель» только показывает модель и расход. Либо запустить `remote-llm` из каталога проекта с тем же токеном (`--token` или `GATEWAY_TOKEN`).
4. Сессия агента выполняется на сервере, в отдельном каталоге этого пользователя и проекта. Файлы проекта уходят туда при старте, изменённые файлы возвращаются после ответа.

---

## Настройки

| Настройка | Где | Что делает |
|---|---|---|
| Адрес шлюза | Плагин и CLI | Куда открывать WebSocket |
| Токен пользователя | Secret Storage плагина и CLI | Кто подключается |
| Принудительная LLM | Админка | Какую модель показывают плагин и CLI |
| Лимит токенов и период сброса | Админка | Сколько токенов доступно пользователю, пока счётчик не обнулится |
| Исходящий прокси | Админка и `.env` | Через какой выход агенты ходят к API провайдеров |

Серверные части рассчитаны на Go. Плагин на JavaScript.

---

## Как это работает

Плагин и CLI тонкие. Они открывают WebSocket, передают протокол ACP и показывают ответ. Сессии, запуск агента, MCP и выполнение команд держит шлюз.

Шлюз смотрит, назначена ли пользователю модель и не исчерпан ли его лимит токенов за текущий период. Затем поднимает backend в каталоге его сессии и проксирует исходящие вызовы через прокси из админки или `.env`.

---

## Разработка

Push и pull request запускают [`.github/workflows/ci.yml`](.github/workflows/ci.yml): `gofmt`, `go vet`, `go test ./...`, проверка и тесты плагина, сборка `gateway`, `api`, `admin` и `remote-llm`. На pull request job образов проверяет сборку `linux/amd64`.

Push в `main` публикует образы `linux/amd64` и `linux/arm64` в GitHub Container Registry. Тег образа — SHA коммита, плюс подвижный тег `main`:

```text
ghcr.io/alanforester/remote-llm-gateway/gateway:<sha>
ghcr.io/alanforester/remote-llm-gateway/api:<sha>
ghcr.io/alanforester/remote-llm-gateway/admin:<sha>
```

Git-тег `vMAJOR.MINOR.PATCH` публикует те же образы с номером версии (`1.2.3` и `v1.2.3`) и тем же SHA. Этот тег собирает GitHub Release: бинарники `remote-llm` для Linux, macOS и Windows и пакет плагина `.vsix`. В описании релиза лежат имена образов этой версии.

```bash
git tag v0.1.2
git push origin v0.1.2
```

Вход в реестр и создание релиза идут через `GITHUB_TOKEN`, который выдаёт сам GitHub Actions. В workflow у этих шагов права `packages: write` и `contents: write`. Отдельного токена в файлах репозитория нет. В Settings → Actions → General у репозитория должно быть разрешено чтение и запись для `GITHUB_TOKEN`, иначе публикация пакета остановится на правах.

Образ собирается из [`deploy/Dockerfile`](deploy/Dockerfile). Без `REDIS_URL` три процесса делят файл `/data/gateway.json`. Процесс в контейнере — пользователь `65532`. В образе `gateway` есть `git`, им пользуются агенты в сессии. `grok` и `claude` в образ не входят, их ставят на машине шлюза. Админке нужен `ADMIN_TOKEN`, без него процесс сразу завершается. Токен WebSocket передаётся заголовком `Authorization`, не в адресе. Nginx не пишет query string в access-лог и не публикует `/metrics`.

В шапке стоят бейджи языка, лицензии, CI и релиза.

Pull request'ы можно предлагать.

---

## Выкладка

На одной машине стек поднимается из [`deploy/docker-compose.yml`](deploy/docker-compose.yml). Внутри Redis, `gateway`, `api`, `admin`, nginx, Prometheus и Grafana. Секреты читаются из `.env` рядом с репозиторием, в образ они не запекаются.

```bash
cp .env.example .env
# свой ADMIN_TOKEN и при необходимости OPENAI_API_KEY, OUTBOUND_PROXY
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build
```

Админка открывается на порту `HTTP_PORT` (по умолчанию 80). `GATEWAY_URL` для плагина и CLI — этот адрес, без `:8080`. Grafana слушает `127.0.0.1:3000`, Prometheus — `127.0.0.1:9090`. Дашборд называется Remote LLM Gateway. Метрики процессов: `remote_llm_http_requests_total`, `remote_llm_tokens_total`, `remote_llm_quota_denied_total`, `remote_llm_sessions`. Nginx их наружу не отдаёт.

`IMAGE_TAG=local` собирает образы из [`deploy/Dockerfile`](deploy/Dockerfile). После публикации в GHCR тот же файл можно поднять без сборки:

```bash
IMAGE_TAG=0.1.2 docker compose --env-file .env -f deploy/docker-compose.yml pull
IMAGE_TAG=0.1.2 docker compose --env-file .env -f deploy/docker-compose.yml up -d
```

`GATEWAY_ECHO=1` в `.env` оставляет эхо. Для настоящих агентов поставьте `0`: `grok` и `claude` должны быть доступны процессу шлюза.

Обновление на VPS — сменить `IMAGE_TAG` на SHA коммита с `main` или на номер релиза и снова выполнить `pull` и `up -d`. Redis и каталог worktree лежат в томах `redis-data` и `workspaces`, их `up` не стирает.

Kubernetes-манифесты — [`deploy/k8s`](deploy/k8s). Туда входят те же процессы. Секрет в репозиторий не кладётся:

```bash
kubectl apply -f deploy/k8s/namespace.yaml
kubectl -n remote-llm create secret generic remote-llm \
  --from-literal=ADMIN_TOKEN=... \
  --from-literal=OPENAI_API_KEY=... \
  --from-literal=OUTBOUND_PROXY= \
  --from-literal=GRAFANA_ADMIN_PASSWORD=...
kubectl apply -k deploy
```

Образ по умолчанию — тег `main`. Другую версию задаёт `newTag` в [`deploy/kustomization.yaml`](deploy/kustomization.yaml). Сервис `nginx` типа LoadBalancer открывает порт 80. На VPS без балансировщика смените тип на NodePort. Grafana остаётся внутри кластера: `kubectl -n remote-llm port-forward svc/grafana 3000:3000`. В манифесте `GATEWAY_ECHO=1`; для боевых агентов поменяйте значение в `deploy/k8s/apps.yaml` и примените kustomization снова. Если кластер не скачивает образ, откройте пакет GHCR или добавьте `imagePullSecret`.

---

## Ограничения

- Плагин в Marketplace не опубликован. Готовый `.vsix` лежит в релизе [v0.1.2](https://github.com/AlanForester/remote-llm-gateway/releases/tag/v0.1.2).
- Вход в аккаунт провайдера делается на сервере шлюза, с рабочей машины пользователя этот вход не проходит.
- GPT подключается через OpenAI Responses API в шлюзе. Отдельного процесса Codex нет.

---

## Лицензия

MIT. Текст в [LICENSE](LICENSE).

Интерфейс плагина опирается на [phuryn/grok-build-vscode](https://github.com/phuryn/grok-build-vscode). У того проекта своя лицензия. Этот репозиторий публикуется под MIT.

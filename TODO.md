# TODO

## Продукт

- [x] **Исходящий прокси для backend-агентов.** Запросы на выходе из Claude Code, Grok Build и остальных агентов идут через прокси. Адрес прокси задаётся двумя способами, и смена любого из них меняет прокси:
  - в веб-админке;
  - переменной в `.env`.
  Шлюз применяет значение к процессам агентов (их исходящие вызовы API провайдеров).
- [x] **Принудительная LLM для пользователя.** В веб-админке пользователю можно принудительно назначить LLM. Плагин и CLI отображают эту назначенную модель.
- [x] **Лимиты использования.** В админке у пользователя задаются лимит токенов и период сброса. Шлюз считает токены периода и не пускает новые запросы после лимита, пока период не сбросится.
- [x] **Синхронизация проекта.** CLI и плагин в начале сессии отправляют файлы на шлюз (`PUT /v1/sessions/{id}/files`) и после ответа записывают правки обратно (`GET /v1/sessions/{id}/changes`). Gitignored-файлы, `.git`, `node_modules` и `vendor` не уходят. `--no-sync` оставляет проект только локально.
- [x] **Инструменты GPT и MCP.** GPT ходит в OpenAI Responses API с tool calling: файлы сессии и команда в её каталоге. Секреты процесса в команду не попадают. Дополнительные инструменты поднимаются на шлюзе из `MCP_CONFIG` (stdio MCP).

## GitHub Actions

- [x] CI на push и pull request: сборка и тесты Go-частей (`gateway`, `api`, `admin`, `cmd/remote-llm`) и плагина на JavaScript. Workflow: `.github/workflows/ci.yml`.
- [x] Линт и проверка форматирования в том же пайплайне: `gofmt`, `go vet`, `node --check`.
- [x] Сборка образов `gateway`, `api` и `admin`. Публикация в GitHub Container Registry: тег по SHA коммита на `main` и semver-тег на релизе. Сами образы появляются после push в `main` или тега.
- [x] Релиз по git-тегу: GitHub Release, бинарники CLI `remote-llm` под целевые ОС, пакет VS Code-плагина (`.vsix`), образы с версией релиза.
- [x] Секреты пайплайна не лежат в репозитории. Реестр и релиз используют встроенный `GITHUB_TOKEN` Actions.
- [ ] **Бейджи в `README.md`.** Статус CI, последний релиз и лицензия (MIT, ссылка на `LICENSE`). Плюс ряд как на референсе: Agents (Grok Build · Codex · Claude Code), VS Code Extension, Cursor Extension. Ссылки CI и релиза ведут на GitHub Actions и релизы репозитория.

## Инфраструктура деплоя и образы

- [x] `Dockerfile` для `gateway`, `api` и `admin`: один `deploy/Dockerfile`, цель сборки задаётся аргументом.
- [x] **Nginx** в `deploy/nginx/nginx.conf`. Один вход: админка на `/`, шлюз на `/v1/` и `/ws/`, API на `/api/`. WebSocket проксируется.
- [x] `docker-compose` в `deploy/`: `gateway`, `api`, `admin`, Redis и nginx. Конфиг и секреты снаружи, через `.env`. Рядом Prometheus и Grafana.
- [x] Пример `.env.example`: исходящий прокси, адреса сервисов, `REDIS_URL` и auth.
- [x] Описание выкладки на VPS или Kubernetes: раздел «Выкладка» в `README.md`, манифесты в `deploy/k8s`, сборка `kubectl apply -k deploy`.
- [x] Мониторинг: `/metrics` у трёх процессов, Prometheus и дашборд Grafana `deploy/grafana/dashboards/gateway.json`.
- [x] Документация релиза: как ставить тег, что попадает в GitHub Release и какие образы после этого использовать. Раздел «Разработка» в `README.md`.

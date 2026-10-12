# План реализации: Remote LLM Gateway (CLI `llm`)

## 1. Цель проекта

Создать **централизованный LLM Gateway** (шлюз), который:

- Запускает Grok Build (и другие агенты: Claude Code, GPT/Codex и т.д.) на одном сервере с одним публичным IP.
- Позволяет **любому количеству пользователей** VS Code Plugin работать **через этот шлюз**, а не через локальный CLI.
- Плагин в VS Code/Cursor работает с **локальным открытым проектом** (workspace), но все запросы, инструменты, MCP, выполнение кода, генерация — идут через удалённый шлюз.
- Поддержка Grok Build, Claude Code, OpenAI (GPT), Anthropic и других через единый протокол.

**Ключевые преимущества:**
- Все пользователи используют один аккаунт/лицензию Grok (или пул).
- Нет необходимости устанавливать тяжёлый Grok Build CLI на машине каждого разработчика.
- Центральное управление, логирование, rate-limiting, кэширование.
- Плагин остаётся лёгким и работает с любым локальным проектом.

## 2. Архитектура решения

### Компоненты

1. **`cmd/llm/`** — CLI-утилита `llm` (TUI, аналог `grok` / `claude`)
2. **Remote LLM Gateway (Server)**
   - Написан на Go или Node.js/TypeScript (рекомендуется Go для производительности и низкого потребления памяти).
   - Запускает несколько экземпляров `grok agent serve` / `grok agent headless` (или через Docker).
   - Предоставляет WebSocket endpoint (`/ws/{sessionId}`) для подключения клиентов.
   - Поддерживает мультиплексирование сессий (один процесс — много клиентов, или per-user sessions).
   - Проксирует ACP (Agent Client Protocol — JSON-RPC 2.0 over WS).
   - Хранит состояние сессий (Redis или in-memory + persistence).
   - Интегрирует MCP servers (memory-mcp и другие) на сервере шлюза.
   - Поддерживает несколько backend-агентов (Grok, Claude Code, GPT-4o via OpenAI Responses API).

2. **VS Code Plugin (модификация https://github.com/phuryn/grok-build-vscode)**
   - Добавляется новая настройка `grok.remoteGatewayUrl` (по умолчанию `wss://gateway.yourdomain.com`).
   - Если `remoteGatewayUrl` указан — вместо спавна локального `grok agent stdio` подключается по WebSocket к шлюзу.
   - Передаёт **локальный workspace root** и информацию о проекте в шлюз при создании сессии.
   - Все команды (edit, read_file, run_terminal, MCP calls) выполняются **на стороне шлюза**, но с правильным cwd = workspace пользователя.
   - Для этого шлюз должен иметь доступ к файлам пользователя — два варианта:
     - **Вариант A (рекомендуемый)**: SSH + git worktree / rsync / VS Code Remote-SSH style (каждый пользователь имеет свой isolated worktree на сервере).
     - **Вариант B**: Плагин использует LSP-like протокол или отдельный local proxy, который шлюз вызывает обратно (сложно).
     - **Вариант C (гибрид)**: Плагин работает в режиме "thin client" — отправляет diff-ы и изменения, шлюз работает в изолированном Docker-контейнере с volume mount по SSHFS или git-push based.

3. **Session Management**
   - При открытии чата в плагине создаётся уникальная сессия (`sessionId = userId + projectHash`).
   - Шлюз запускает `grok agent serve --secret=...` или использует persistent headless connection.
   - Плагин подключается: `wss://gateway/.../ws/{sessionId}?token=...&workspace=/path/to/local/project`.
   - Шлюз маппит workspace path на серверную директорию (например, `/workspaces/user123-projectabc`).

4. **Поддержка нескольких агентов**
   - Gateway имеет конфиг:
     ```yaml
     backends:
       grok:
         type: grok-build
         command: ["grok", "agent", "serve"]
       claude:
         type: claude-code
         command: ["claude", "agent", "serve"]
       gpt:
         type: openai-responses
         apiKey: ...
     ```
   - Плагин позволяет выбирать модель/агента в UI (как сейчас).

## 3. Подробный план реализации (этапы)

### Этап 1: Дизайн и спецификация (текущий документ)
- Определить протокол между Plugin и Gateway (расширенный ACP + metadata о workspace).
- Решить модель изоляции файлов (worktree per session — лучший баланс безопасности/производительности).
- Определить аутентификацию (JWT + user-specific tokens).

### Этап 2: Разработка LLM Gateway (2-3 недели)
- Создать репозиторий `grok-llm-gateway`.
- Использовать Go + gorilla/websocket или TypeScript + ws.
- Реализовать:
  - WebSocket ACP proxy/router.
  - Session manager (Redis для масштабирования).
  - Worktree manager (`git worktree add` per session).
  - Backend spawner (`grok agent serve` в background per worktree).
  - MCP proxy (memory-mcp и другие запускаются на сервере).
  - Rate limiting, logging, usage analytics.
  - Docker + docker-compose для лёгкого деплоя.
- Поддержка `grok agent headless --grok-ws-url` для outbound.

### Этап 3: Модификация VS Code Plugin (1-2 недели)
- Форкнуть https://github.com/phuryn/grok-build-vscode.
- Добавить настройки:
  - `grok.useRemoteGateway`: boolean
  - `grok.gatewayUrl`: string (wss://...)
  - `grok.gatewayToken`: string (секрет)
- Изменить транспорт:
  - Если remote — создать WebSocket connection вместо child_process.spawn('grok').
  - Адаптировать JSON-RPC client для WS.
  - При создании сессии отправлять `initialize` с `workspaceRoot`, `projectName`.
- Добавить UI для выбора "Local CLI" / "Remote Gateway".
- Поддержать все текущие фичи (permissions, diffs, @-mentions, imagine, MCP tools).
- Сделать backward compatible.

### Этап 4: Модель изоляции и безопасность
- Каждый пользователь/проект получает isolated git worktree на сервере шлюза.
- Шлюз делает `cd /workspaces/{user}-{project}` перед запуском агента.
- Все команды `read_file`, `search_replace`, `run_terminal_command` выполняются внутри worktree.
- Синхронизация изменений обратно в локальный проект пользователя:
  - После каждого edit — шлюз может возвращать patch.
  - Или плагин периодически делает `git pull` из remote worktree (через SSH key).
  - Лучший вариант: использовать `grok-build` feature worktrees + `git push` в private repo пользователя.

### Этап 5: Поддержка Claude Code и GPT
- Для Claude Code — аналогично, если есть `claude agent serve`.
- Для GPT — использовать OpenAI Responses API + tool calling напрямую в gateway (или через `grok-build-providers`).
- Единый интерфейс в плагине.

### Этап 6: Деплой и документация
- Деплой на VPS (Hetzner, DigitalOcean) или Kubernetes.
- Автоматическое масштабирование сессий.
- Документация:
  - Как развернуть Gateway.
  - Как настроить плагин.
  - Security best practices (isolated worktrees, no shared keys).
- Примеры конфигурации.

## 4. Технические вызовы и решения

**Вызов** | **Решение**
--- | ---
Доступ к локальным файлам пользователя | Isolated git worktrees + bidirectional sync (patch + git)
Сложность ACP протокола | Использовать существующий клиент из phuryn/grok-build-vscode как основу
Масштабирование (много пользователей) | Redis для сессий + per-user Docker containers или worktrees
Стоимость Grok аккаунта | Один SuperGrok/Premium+ аккаунт на шлюзе + careful rate limiting
Безопасность | JWT auth, isolated worktrees, no shell escape, audit logs

## 5. Следующие шаги (рекомендуется прямо сейчас)

1. Создать новый репозиторий `llm-gateway` (или `grok-remote-gateway`).
2. Написать `README.md` и `ARCHITECTURE.md` на основе этого плана.
3. Сделать PoC:
   - Простой WebSocket proxy, который запускает `grok agent serve` и проксирует JSON-RPC.
   - Модифицировать плагин для подключения по WS вместо stdio.
4. Протестировать с одним локальным проектом.
5. Добавить поддержку worktree sync.

## 6. Технологический стек (рекомендация)

- **Gateway**: Go (fiber или gin) + gorilla/websocket
- **Plugin**: TypeScript (существующий код phuryn)
- **State**: Redis
- **Isolation**: Git worktrees + Docker (опционально)
- **Auth**: JWT + API keys
- **Monitoring**: Prometheus + Grafana

Этот план позволяет всем пользователям плагина работать через **один** шлюз с одного IP, сохраняя удобство работы с локальным проектом в VS Code.

**Автор документа:** Grok Build (по запросу пользователя)
**Дата:** 12 октября 2026

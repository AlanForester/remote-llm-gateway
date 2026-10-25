# Remote LLM Gateway

A chat in the right sidebar, next to Grok. The project stays on your machine: at the start of a session the files go to the gateway, and after the reply the edits are written back into the editor.

The plugin does not start a local `grok` or `claude`. The model, the limit, and the proxy are set in the gateway admin UI.

## Settings

| Setting | What it does |
|---|---|
| `remoteLlm.gatewayUrl` | Gateway address, `http` or `ws` |
| `remoteLlm.useRemoteGateway` | Connect to the gateway |

The user token is saved with the command «Remote LLM: задать токен» and stored in the editor's Secret Storage, not in `settings.json`. «Remote LLM: показать назначенную модель» shows the model and usage.

Repository: https://github.com/AlanForester/remote-llm-gateway

License: MIT.

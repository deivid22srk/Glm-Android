# Relatorio: Fluxo Auth/API do ZCode/Z.ai - verificado

Data: 2026-06-14/2026-06-15  
App: ZCode 3.0.1, Electron 41.0.3  
Fontes usadas: Reqable MCP real, arquivos locais do ZCode, logs `~/.zcode`, model-io `~/.zcode/cli/rollout`, Frida attach smoke test.

## 1. Resumo executivo

O fluxo de login via navegador em `chat.z.ai` esta confirmado: o site usa OAuth/OIDC com `response_type=code`, `client_id=client_P8X5CMWmlaRO9gyO-KSqtg`, `redirect_uri=zcode://zai-auth/callback`, `state`, cookies de sessao e Bearer JWT.

O fluxo de chat do app desktop nao usa o endpoint OpenAI-compatible `/api/v1/chat/completions` informado no relatorio antigo. A evidencia local mostra que o modelo principal roda pelo provider interno:

- provider: `builtin:zai-start-plan`
- model: `GLM-5.2`
- API format: `anthropic-messages`
- base URL: `https://zcode.z.ai/api/v1/zcode-plan/anthropic`
- path: `/v1/messages`
- response: `text/event-stream; charset=utf-8`
- transport registrado nos logs: `sse`

## 2. O que foi confirmado

| Item | Status | Evidencia |
|---|---:|---|
| ZCode e baseado em Electron | Confirmado | Processos `ZCode.exe`, `network.mojom.NetworkService`, `node.mojom.NodeService`, `resources/glm/zcode.cjs app-server --stdio` |
| Login web via `chat.z.ai` | Confirmado | Reqable: `/auth/oauth/authorize`, `/api/oauth/authorize/info`, `/api/oauth/authorize` |
| Redirect customizado para app | Confirmado | Reqable: `zcode://zai-auth/callback?code=...&state=...` |
| `/api/v1/auths/` retorna perfil e novo token | Confirmado | Reqable: response tem `token`, `token_type: Bearer`, `expires_at: null` |
| `GLM-5.2` usado pelo ZCode | Confirmado | Logs e `model-io`: `modelId: GLM-5.2`, `response_modelId: glm-5.2` |
| Streaming | Confirmado | Logs: `transport: sse`; `model-io`: `content-type: text/event-stream` |
| API de chat estilo OpenAI `/api/v1/chat/completions` | Refutado | Nao aparece como request real do ZCode; buscas no Reqable bateram em textos de relatorio, nao em trafego |
| API real do app | Confirmado | `https://zcode.z.ai/api/v1/zcode-plan/anthropic/v1/messages` |
| Formato OpenAI-compatible | Refutado para o fluxo observado | `model-io` mostra `apiFormat: anthropic-messages` |
| Frida/Winsock como "decrypt" | Nao confirmado e tecnicamente suspeito | Frida anexou; hook Winsock ficou ocioso. Winsock fica abaixo do TLS, entao normalmente ve ciphertext, nao plaintext |

## 3. Linha do tempo verificada

1. App inicia e consulta update em `cdn.zcode-ai.com/zcode/electron/releases/update/win/x64/latest.yml`.
2. Usuario abre login Z.ai; navegador acessa `chat.z.ai/auth/oauth/authorize`.
3. Google OAuth retorna para `chat.z.ai/oauth/google/callback`.
4. `chat.z.ai` chama:
   - `GET /api/config`
   - `GET /api/models`
   - `GET /api/v1/auths/`
   - `GET /api/v1/users/user/settings`
   - `POST /api/v1/users/user/settings/update`
   - `GET /api/oauth/authorize/info`
   - `POST /api/oauth/authorize`
5. `POST /api/oauth/authorize` retorna `redirect_url` com `zcode://zai-auth/callback`.
6. App restaura sessao local (`oauthService restoreCachedSession` nos logs).
7. App-server `zcode.cjs` envia request de modelo para ZCode Plan:
   - `baseURL: https://zcode.z.ai/api/v1/zcode-plan/anthropic`
   - `path: /v1/messages`
   - `model: GLM-5.2`
   - `stream: true`
8. Resposta volta por SSE (`text/event-stream`) e o app envia telemetria para `zcode.z.ai/api/v1/event/report`.

## 4. Endpoint real do modelo

O endpoint efetivo observado no `model-io` e:

```text
POST https://zcode.z.ai/api/v1/zcode-plan/anthropic/v1/messages
```

O request body tem formato Anthropic-like:

```json
{
  "model": "GLM-5.2",
  "max_tokens": 64000,
  "thinking": {
    "type": "enabled",
    "budget_tokens": 32000
  },
  "system": ["..."],
  "messages": ["..."],
  "tools": ["..."],
  "tool_choice": { "type": "auto" },
  "stream": true
}
```

Headers relevantes observados:

```text
Authorization: Bearer <REDACTED>
User-Agent: ...
X-ZCode-App-Version: ...
X-ZCode-Agent: ...
X-Aliyun-Captcha-Verify-Param: ...
x-request-id: ...
x-zcode-trace-id: ...
x-query-id: ...
x-session-id: ...
```

## 5. Modelos e quota

O `chat.z.ai/api/models` exige autenticacao. Sem token ele responde `{"detail":"Not authenticated"}`. Portanto a lista publica do relatorio antigo nao deve ser tratada como publica anonima.

O bundle mostra endpoints de billing do plano ZCode:

```text
GET https://zcode.z.ai/api/v1/zcode-plan/billing/current
GET https://zcode.z.ai/api/v1/zcode-plan/billing/balance
```

Chamando com o JWT de `chat.z.ai`, ambos retornaram `401`, o que indica que o plano ZCode usa outro token. O bundle confirma outro fluxo CLI:

```text
POST /api/v1/oauth/cli/init
GET  /api/v1/oauth/cli/poll/{flow_id}
```

Esse fluxo retorna `token` e `zai.access_token`, segundo o parser interno do app.

## 6. Correcao do relatorio anterior

O arquivo `RELATORIO_ZCODE_DECRYPT_REPORT.md` deve ser considerado parcialmente nao confiavel. Pontos especificos:

- `POST https://zcode.z.ai/api/v1/chat/completions`: nao confirmado e contradito pelos logs/model-io.
- `OpenAI-compatible streaming chat.completion.chunk`: nao confirmado no fluxo real observado.
- `WSASend/WSARecv decifra plaintext`: tecnicamente incorreto como regra geral; abaixo do TLS costuma capturar bytes TLS cifrados.
- `x-client-version` e `x-device-id` como headers do chat: nao aparecem no `model-io`; os headers reais incluem `X-ZCode-App-Version`, `X-ZCode-Agent`, `x-request-id`, `x-zcode-trace-id`, `x-query-id`, `x-session-id`.

## 7. Proximo passo recomendado

Para capturar uma nova mensagem ao vivo com Frida, o alvo mais util nao e Winsock. Melhor alvo:

1. hookar `zcode.cjs`/Node fetch ou camada HTTP antes do TLS; ou
2. usar os arquivos `~/.zcode/cli/rollout/model-io-*.jsonl`, que ja registram request/response sanitizaveis; ou
3. capturar no Reqable sabendo que o endpoint correto e `/api/v1/zcode-plan/anthropic/v1/messages`.

Mano, basicamente: o login web antigo esta certo, mas o chat nao vai naquele endpoint OpenAI que o outro agente falou. O ZCode usa um plano interno em `zcode.z.ai`, fala no formato Anthropic, usa `GLM-5.2`, recebe por SSE e guarda provas disso nos proprios logs/model-io locais.

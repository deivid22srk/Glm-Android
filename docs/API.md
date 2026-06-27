# Administrative API

Base URL padrão:

```text
http://127.0.0.1:3005
```

## Estado

```text
GET /health
GET /api/admin/overview
GET /api/admin/settings
PATCH /api/admin/settings
```

`PATCH /api/admin/settings` aceita:

```json
{ "port": 3005, "apiEnabled": true }
```

`apiEnabled=false` para apenas a superficie OpenAI-compatible. As rotas
administrativas continuam respondendo para o painel Wails.

## Contas

```text
GET    /api/admin/accounts
GET    /api/admin/accounts/{id}
POST   /api/admin/accounts/{id}/activate
PUT    /api/admin/accounts/order
DELETE /api/admin/accounts/{id}
GET    /api/admin/queue
```

A listagem consulta a cota de todas as contas em paralelo. Tokens e segredos
nunca são retornados.

Reordenar a fila:

```json
{ "accountIds": ["account-1", "account-2"] }
```

A ordem salva define `Conta 1`, `Conta 2` e tambem a ordem usada pela rotacao
automatica quando a cota da conta ativa esgota.

`GET /api/admin/queue` mostra requests em execucao e aguardando por chave
`conta:modelo`. O proxy serializa requests da mesma conta e do mesmo modelo
para evitar `model concurrency limit exceeded` no ZCode.

## Login

```text
POST /api/admin/auth/login/start
GET  /api/admin/auth/login/poll?flow_id={flowId}
```

Resposta inicial:

```json
{
  "flowId": "...",
  "authorizeUrl": "https://...",
  "expiresAt": "...",
  "pollIntervalSec": 2,
  "status": "pending"
}
```

O frontend deve abrir `authorizeUrl` no navegador e consultar o endpoint de
poll respeitando `pollIntervalSec`.

## Cota

```text
GET /zcode/quota
GET /zcode/quota/accounts?model=glm-5.2
```

Cada balance retorna números da nuvem e `usagePercent`, calculado como:

```text
used / total * 100
```

`GET /api/admin/accounts` embute o snapshot de cota de cada conta em
`account.quota.balances[]`, incluindo GLM-5.2 e GLM-5-Turbo quando a nuvem
retorna os dois saldos.

## Modelos

```text
GET /api/admin/models/capabilities
GET /v1/models
```

O endpoint administrativo retorna capacidades completas. `/v1/models` segue o
formato OpenAI.

## Thinking

Global:

```text
GET /api/admin/thinking
PUT /api/admin/thinking
```

Por conta:

```text
GET    /api/admin/accounts/{id}/thinking
PUT    /api/admin/accounts/{id}/thinking
DELETE /api/admin/accounts/{id}/thinking
```

Payload:

```json
{
  "enabled": true,
  "budgetTokens": 32000,
  "effort": "max"
}
```

Valores de `effort`:

```text
none
low
medium
high
max
```

## API keys

```text
GET    /api/admin/api-keys
POST   /api/admin/api-keys
DELETE /api/admin/api-keys/{id}
```

Criação:

```json
{ "name": "Roo Code" }
```

Resposta:

```json
{
  "apiKey": {
    "id": "...",
    "name": "Roo Code",
    "prefix": "zkp_...",
    "createdAt": "..."
  },
  "secret": "zkp_...",
  "warning": "The secret is returned only once."
}
```

## OpenAI-compatible

```text
POST /v1/chat/completions
POST /chat/completions
GET  /v1/models
```

Suporta:

- streaming e non-streaming;
- tools/function calling;
- system/developer messages;
- resultados de tools;
- imagens data URL;
- `temperature`, `top_p` e `stop`;
- aliases dos dois modelos;
- API key local.

## Captcha

```text
GET  /zcode/captcha/config
GET  /zcode/captcha/browser
GET  /zcode/captcha/poll
POST /zcode/captcha/submit
POST /zcode/captcha/test
```

Os endpoints `poll` e `submit` são internos ao broker.

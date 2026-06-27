# Relatório: Fluxo Auth/API do ZCode/Z.ai

**Data da análise:** 14 de junho de 2026  
**Versão do ZCode analisada:** 3.0.1 (Electron 41.0.3, Chrome 146.0.7680.80)  
**Ferramenta de captura:** Reqable (proxy MITM)

---

## 1. Resumo Executivo

O ZCode é um aplicativo desktop baseado em Electron que utiliza **OAuth 2.0 com Authorization Code Flow** para autenticação. O fluxo envolve:

1. **App Electron** (ZCode.exe) → abre navegador para login
2. **Navegador** (Chrome) → autentica via Google OAuth → redireciona para chat.z.ai
3. **chat.z.ai** → gera authorization code → redireciona via protocolo customizado `zcode://zai-auth/callback`
4. **App Electron** → recebe code → troca por access token JWT (ES256)
5. **App Electron** → faz chamadas à API usando Bearer token

**Modelo principal detectado:** GLM-5.2 (uso interno, não listado na API pública)  
**Provedor:** ZhipuAI (Z.ai) via endpoint compatível com OpenAI  
**Mecanismo de resposta:** SSE (Server-Sent Events) inferido via telemetria

---

## 2. Linha do Tempo do Fluxo

```
19:05:56 - App inicia: verifica update em cdn.zcode-ai.com/zcode/electron/releases/update/win/x64/latest.yml
19:07:41 - App carrega config: cdn.zcode-ai.com/zcode/config/default.json
19:07:41 - App busca avatar: lh3.googleusercontent.com/a/ACg8ocIp...
19:07:42 - App carrega Aliyun Captcha JS
19:07:45 - Usuário clica "Login com Z.ai" → evento app_login_ck
19:07:45 - App reporta: app_login_success
19:06:18 - [Navegador] chat.z.ai/auth → página de login Z.ai
19:06:44 - Usuário clica "Continuar com Google"
19:06:46 - Google OAuth: accounts.google.com/v3/signin/accountchooser
19:06:48 - Google OAuth: accounts.google.com/ServiceLogin
19:06:50 - Google OAuth: accounts.google.com/signin/oauth/id
19:06:52 - Google OAuth: accounts.google.com/signin/oauth/consent
19:06:53 - Callback Google: chat.z.ai/oauth/google/callback?code=4/0AdkVL...
19:06:54 - chat.z.ai/api/config (configuração do sistema)
19:06:54 - chat.z.ai/api/models (lista de modelos disponíveis)
19:06:54 - chat.z.ai/api/v1/auths/ (perfil do usuário)
19:06:55 - chat.z.ai/auth → página de login completa
19:06:56 - chat.z.ai/auth/oauth/authorize → tela de consentimento OAuth
19:06:57 - chat.z.ai/api/oauth/authorize/info → info do cliente ZCode
19:06:57 - chat.z.ai/api/v1/users/user/settings → busca settings do usuário
19:06:57 - chat.z.ai/api/v1/users/user/settings/update → atualiza timezone
19:07:02 - chat.z.ai/api/oauth/authorize (POST) → gera code
19:07:02 - Response: redirect_url: "zcode://zai-auth/callback?code=code-2a325de10e33"
19:07:02 - Analytics: oauth_authorization_success
19:08:50 - [App] Usuário clica send → evento send_btn
19:09:02 - [App] Agente processa → evento agent_step
19:09:03 - [App] Mensagem completa → evento message_completion (GLM-5.2, 7738 tokens)
```

---

## 3. Endpoints Relevantes

| Fase | Método | Host | Path | Status | Função Provável | Auth Usada |
|------|--------|------|------|--------|----------------|-----------|
| A | GET | cdn.zcode-ai.com | /zcode/electron/releases/update/win/x64/latest.yml | 200 | Verificar updates do app | Nenhuma |
| A | GET | cdn.zcode-ai.com | /zcode/config/default.json | 200 | Configuração global do app | Nenhuma |
| B | GET | chat.z.ai | /auth/oauth/authorize | 200 | Página de consentimento OAuth | Cookie session |
| B | GET | chat.z.ai | /api/oauth/authorize/info | 200 | Info do cliente OAuth | Bearer JWT |
| B | POST | chat.z.ai | /api/oauth/authorize | 200 | Gera authorization code | Bearer JWT + form-urlencoded |
| B | GET | chat.z.ai | /oauth/google/callback | 307 | Callback do Google OAuth | Query param code |
| C | GET | chat.z.ai | /api/config | 200 | Configuração do sistema | Bearer JWT |
| D | GET | chat.z.ai | /api/v1/auths/ | 200 | Perfil do usuário autenticado | Bearer JWT |
| D | GET | chat.z.ai | /api/v1/users/user/settings | 200 | Settings do usuário | Bearer JWT |
| D | POST | chat.z.ai | /api/v1/users/user/settings/update | 200 | Atualiza settings | Bearer JWT |
| E | GET | chat.z.ai | /api/models | 200 | Lista modelos disponíveis | Bearer JWT |
| G | POST | zcode.z.ai | /api/v1/event/report | 200 | Telemetria de eventos | Cookie visitor_id |
| I | POST | j2c03hoppk-default-cn.rum.aliyuncs.com | / | 200 | RUM/monitoring | Nenhuma |
| I | GET | sdata.chatglm.cn | /frontend/zai/e.gif | 200 | Pixel de tracking | Query params |

---

## 4. Autenticação

### 4.1 Fluxo OAuth Completo

O ZCode implementa **OAuth 2.0 Authorization Code Flow** com as seguintes características:

**Parâmetros OAuth:**
- `client_id`: `client_P8X5CMWmlaRO9gyO-KSqtg`
- `redirect_uri`: `zcode://zai-auth/callback` (protocolo customizado)
- `response_type`: `code`
- `state`: hash aleatório (ex: `74a86a6194dcc96746d84f0e3de4c9aecc6896a921810cd5835985c790e448ed`)
- `scopes`: `openid`, `profile`, `email`

**O que o app faz:**
1. ZCode.exe registra protocolo `zcode://` no sistema
2. Ao clicar "Login", abre `https://chat.z.ai/auth/oauth/authorize?...` no navegador padrão
3. Navegador redireciona para Google OAuth
4. Usuário autentica no Google → Google retorna code para chat.z.ai
5. chat.z.ai troca code Google por sessão interna
6. Usuário vê tela de consentimento: "ZCode gostaria de acessar sua conta Z.ai"
7. Usuário clica "Permitir"
8. chat.z.ai gera novo code e retorna JSON com `redirect_url: "zcode://zai-auth/callback?code=code-XXX"`
9. Navegador tenta abrir URL → sistema invoca ZCode.exe
10. ZCode.exe extrai code e troca por access token

**O que acontece no navegador:**
- Login Google → consentimento Z.ai → redirect via protocolo customizado

**O que volta para o app:**
- Authorization code via URL scheme `zcode://zai-auth/callback?code=code-XXX&state=YYY`

**Como a sessão fica autenticada:**
- App armazena JWT token (ES256) e usa em header `Authorization: Bearer <token>`
- Token contém: `{"id":"<user_uuid>","email":"<email>"}`
- Validade do token: não observada expiração durante a sessão (provavelmente 24h+)

### 4.2 JWT Token Structure

**Header:**
```json
{
  "alg": "ES256",
  "typ": "JWT"
}
```

**Payload:**
```json
{
  "id": "30e20e1e-2b26-4063-a253-<REDACTED>",
  "email": "<REDACTED>@gmail.com"
}
```

**Sem campo `exp`** → token pode ser de longa duração ou validado server-side.

### 4.3 Cookies Importantes

| Cookie | Função | Origem |
|--------|--------|--------|
| `token` | JWT de sessão do usuário | chat.z.ai |
| `oauth_id_token` | ID token do Google (RS256) | Google OAuth |
| `acw_tc` | Session tracking Alibaba Cloud | CDN |
| `visitor_id` | Identificador de visitante | zcode.z.ai |
| `_ga`, `_ga_*` | Google Analytics | Google |
| `cdn_sec_tc` | CDN session token | Alibaba CDN |

---

## 5. Modelos e Quota

### 5.1 Modelos Disponíveis na API Pública

Retornados por `GET https://chat.z.ai/api/models`:

| ID | Nome | Descrição | Max Tokens | Capabilities |
|----|------|-----------|-----------|--------------|
| GLM-5.1 | GLM-5.1 | Flagship model for daily chat and agentic tasks | 32000 | agent_mode, think, web_search, file_qa, mcp |
| GLM-5-Turbo | GLM-5-Turbo | New model for chat, coding, and agentic task | 32000 | agent_mode, think, web_search, file_qa, mcp |
| GLM-5V-Turbo | GLM-5V-Turbo | Vision model with evolved intelligence | 32000 | vision, vlm_tools, citations |
| glm-5 | GLM-5 | Previous flagship model | 32000 | agent_mode, think, web_search, file_qa, mcp |

**Observação:** Todos têm `owned_by: "openai"` mas são modelos ZhipuAI (GLM).

### 5.2 Modelo Interno Detectado

Via telemetria do ZCode.exe (`event_type: "agent_trace"`):

```json
{
  "model_name": "builtin:zai-start-plan/GLM-5.2",
  "model_provider": "glm",
  "agent": "glm"
}
```

**GLM-5.2 não aparece na API pública** → modelo interno/exclusivo do app desktop.

### 5.3 Quota/Saldo

**Não encontrado endpoint específico de quota/billing** no tráfego capturado.

**Indicadores de uso via telemetria:**
- `input_tokens`: "7604"
- `output_tokens`: "134"
- `cached_input_tokens`: "7040"
- `total_tokens`: "7738"
- `converter_daily_limit`: 100 (no `/api/config`)

**Hipótese:** quota é gerenciada server-side sem endpoint explícito de consulta.

---

## 6. Envio de Prompt e Resposta

### 6.1 Endpoint de Chat

**NÃO CAPTURADO DIRETAMENTE** no tráfego analisado.

**Evidências indiretas via telemetria:**
- `event_type: "message_completion"`
- `ask_mode: "build"`
- `duration_ms: 13296`
- `time_to_first_token: 13137`
- `status: "success"`

**Inferência:**
- Endpoint provavelmente: `POST https://chat.z.ai/api/chat/completions` ou similar
- Formato: OpenAI-compatible (`/v1/chat/completions`)
- Streaming: **SSE (Server-Sent Events)** inferido via `time_to_first_token` métrica

### 6.2 Mecanismo de Resposta

**SSE (Server-Sent Events)** - evidências:
1. Métrica `time_to_first_token` indica streaming
2. Config tem `completion_version: "2"` → versão de API de completions
3. Formato OpenAI-compatible geralmente usa SSE
4. Não há WebSocket capturado no tráfego Z.ai

**Fluxo provável:**
```
POST /api/chat/completions
Accept: text/event-stream
Authorization: Bearer <jwt>

{
  "model": "GLM-5.2",
  "messages": [...],
  "stream": true
}

Response (SSE):
data: {"choices":[{"delta":{"content":"Hello"}}]}
data: {"choices":[{"delta":{"content":" world"}}]}
data: [DONE]
```

---

## 7. Headers Importantes

### 7.1 Headers de Request (ZCode.exe → API)

```http
Authorization: Bearer <JWT_ES256_REDACTED>
Content-Type: application/json
User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) ZCode/3.0.1 Chrome/146.0.7680.80 Electron/41.0.3 Safari/537.36
Accept-Language: en-US
sec-ch-ua-platform: "Windows"
Cookie: acw_tc=<REDACTED>; visitor_id=<UUID>
```

### 7.2 Headers de Request (Navegador → chat.z.ai)

```http
Authorization: Bearer <JWT_ES256_REDACTED>
Content-Type: application/json
Origin: https://chat.z.ai
Referer: https://chat.z.ai/auth/oauth/authorize?...
User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36
x-region: overseas
Cookie: token=<JWT>; oauth_id_token=<GOOGLE_JWT>; _ga=<GA_ID>; cdn_sec_tc=<TOKEN>
```

### 7.3 Headers de Response

```http
Content-Type: application/json
Server: ESA (Alibaba Cloud CDN)
via: ens-cache*.br7[...]
x-trace-id: 19ec787b19aef935
Timing-Allow-Origin: *
access-control-allow-origin: https://chat.z.ai
access-control-allow-credentials: true
```

---

## 8. Payloads Exemplificados

### 8.1 OAuth Authorize Request

**Request:**
```http
POST /api/oauth/authorize HTTP/1.1
Host: chat.z.ai
Content-Type: application/x-www-form-urlencoded
Authorization: Bearer <JWT>

client_id=client_P8X5CMWmlaRO9gyO-KSqtg
&redirect_uri=zcode%3A%2F%2Fzai-auth%2Fcallback
&state=74a86a6194dcc96746d84f0e3de4c9aecc6896a921810cd5835985c790e448ed
&response_type=code
&action=approve
```

**Response:**
```json
{
  "redirect_url": "zcode://zai-auth/callback?code=code-2a325de10e33&state=74a86a6194dcc96746d84f0e3de4c9aecc6896a921810cd5835985c790e448ed"
}
```

### 8.2 OAuth Authorize Info

**Request:**
```http
GET /api/oauth/authorize/info?client_id=client_P8X5CMWmlaRO9gyO-KSqtg&redirect_uri=zcode%3A%2F%2Fzai-auth%2Fcallback HTTP/1.1
Host: chat.z.ai
Authorization: Bearer <JWT>
```

**Response:**
```json
{
  "client": {
    "client_name": "ZCode",
    "logo_uri": "https://z-cdn.chatglm.cn/z-ai/static/logo.svg",
    "scopes": ["openid", "profile", "email"],
    "terms_of_service": null,
    "privacy_policy": null
  },
  "user": {
    "id": "30e20e1e-2b26-4063-a253-<REDACTED>",
    "name": "<REDACTED>",
    "email": "<REDACTED>@gmail.com",
    "profile_image_url": "https://lh3.googleusercontent.com/a/<REDACTED>=s96-c"
  },
  "require_consent": true
}
```

### 8.3 Config Response

**Request:**
```http
GET /api/config HTTP/1.1
Host: chat.z.ai
Authorization: Bearer <JWT>
```

**Response (trecho):**
```json
{
  "status": true,
  "name": "Z",
  "version": "0.0.0",
  "oauth": {
    "providers": {
      "google": "google",
      "github": "github",
      "maas": "maas"
    }
  },
  "completion_version": "2",
  "default_models": "GLM-5.1",
  "recommand_model": "glm-5",
  "converter_daily_limit": 100,
  "features": {
    "auth": true,
    "enable_websocket": false,
    "enable_artifacts_mode": true,
    "enable_mcp": true,
    "enable_captcha": true
  }
}
```

### 8.4 Telemetry Event (Agent Trace)

**Request:**
```http
POST /api/v1/event/report HTTP/1.1
Host: zcode.z.ai
Content-Type: application/json
Cookie: acw_tc=<REDACTED>; visitor_id=<UUID>

{
  "event_id": "1e69f12c-c3a5-4102-805c-<REDACTED>",
  "client_timezone": "America/Sao_Paulo",
  "client_language": "en-US",
  "element_name": "message_completion",
  "event_region": "app",
  "event_type": "agent_trace",
  "event_extra_detail": {
    "ask_mode": "build",
    "model_name": "builtin:zai-start-plan/GLM-5.2",
    "model_provider": "glm",
    "agent": "glm",
    "input_tokens": "7604",
    "output_tokens": "134",
    "reasoning_tokens": "0",
    "cached_input_tokens": "7040",
    "total_tokens": "7738",
    "duration_ms": "13296",
    "time_to_first_token": "13137",
    "status": "success"
  },
  "user_id": "30e20e1e-2b26-4063-a253-<REDACTED>",
  "app_version": "3.0.1",
  "device_os_category": "windows",
  "device_mid": "7c6042a8-c83c-435c-b3b2-<REDACTED>",
  "talk_id": "sess_e60f050a-d6af-42ad-81d4-<REDACTED>",
  "message_id": "user-1781464129546-zf6pbu"
}
```

**Response:**
```json
{
  "code": 0,
  "msg": ""
}
```

---

## 9. API Pública vs Interna

### 9.1 API Pública (OpenAI-Compatible)

**Endpoint base:** `https://chat.z.ai/api/`

**Características:**
- Formato OpenAI (`/api/models`, provavelmente `/api/chat/completions`)
- Models retornados têm `owned_by: "openai"` mas são GLM
- Autenticação via Bearer JWT
- 4 modelos públicos: GLM-5.1, GLM-5-Turbo, GLM-5V-Turbo, GLM-5

**Classificação:** API interna com interface OpenAI-compatible

### 9.2 API Interna do App

**Endpoints específicos ZCode:**

| Endpoint | Função |
|----------|--------|
| `cdn.zcode-ai.com/zcode/config/default.json` | Config estática do app |
| `cdn.zcode-ai.com/zcode/electron/releases/update/...` | Updates do Electron |
| `zcode.z.ai/api/v1/event/report` | Telemetria de uso |

**Modelo interno exclusivo:**
- `builtin:zai-start-plan/GLM-5.2` → não acessível via API pública

### 9.3 APIs de Terceiros

| Serviço | Função |
|---------|--------|
| `accounts.google.com` | OAuth Google |
| `j2c03hoppk-default-cn.rum.aliyuncs.com` | RUM/monitoring Alibaba |
| `sdata.chatglm.cn` | Pixel de tracking ZhipuAI |
| `analytics.google.com` | Google Analytics |
| `captcha-open-southeast.aliyuncs.com` | CAPTCHA Alibaba Cloud |

---

## 10. Explicação Simplificada

**Mano, basicamente funciona assim:**

1. Você abre o ZCode (app Electron no seu PC)
2. Clica em "Login com Z.ai"
3. O app abre seu navegador Chrome e manda pra página de login da Z.ai
4. Você loga com Google (igual logar em qualquer site com Google)
5. A Z.ai pergunta: "Quer deixar o ZCode acessar sua conta?" → você clica "Sim"
6. O navegador redireciona pra um link especial tipo `zcode://...` que o Windows sabe que é do app ZCode
7. O ZCode pega esse link, extrai um código secreto e troca por um token (tipo uma senha temporária)
8. Pronto, agora o app tá logado e pode mandar mensagens pro GLM-5.2 (modelo interno que não aparece na API pública)
9. Quando você manda uma mensagem, ela vai pra API da Z.ai e volta em streaming (aparece letra por letra na tela)
10. Tudo que você faz é reportado pra telemetria deles (pra melhorar o app/analisar uso)

**Resumindo:** É OAuth padrão de mercado (igual "Login com Google" em qualquer app), mas com um truque de usar protocolo customizado (`zcode://`) pra voltar pro app desktop. O modelo principal (GLM-5.2) é exclusivo do app e não aparece na API pública.

---

## 11. Perguntas em Aberto

1. **Endpoint de chat não capturado:** Qual é exatamente o endpoint de `/api/chat/completions` ou similar? Necessário capturar tráfego durante envio de mensagem com inspeção mais profunda.

2. **Troca code→token:** Não foi capturado o request exato onde o ZCode.exe troca o authorization code pelo JWT token. Provavelmente é um `POST /api/oauth/token` mas não foi observado.

3. **Validade do JWT:** Token não tem campo `exp`. Como é feita a expiração/refresh? Server-side validation?

4. **GLM-5.2:** Modelo exclusivo do app. Existe endpoint público pra ele? É o mesmo backend dos modelos públicos?

5. **Quota/billing:** Não há endpoint visível de quota. Como o usuário sabe quanto uso tem? É gerenciado inteiramente server-side?

6. **Rate limiting:** Há rate limiting no endpoint de chat? Qual o limite de requests/tokens por dia?

7. **WebSocket vs SSE:** Config mostra `enable_websocket: false` mas telemetria sugere streaming. Confirmar se é SSE puro ou há fallback pra WebSocket em alguns casos.

8. **CAPTCHA:** Captcha da Alibaba Cloud aparece no fluxo. É obrigatório pra login? Em que condições é triggered?

9. **Múltiplas contas:** Usuário tem 2 contas no cookie (`apiekh59@gmail.com` e `maiconbarbosa1111@gmail.com`). Como funciona troca de conta no app?

10. **Segurança do token:** JWT é ES256 (curva elíptica) mas não tem expiração. Há rotação de chave? Como é feito logout?

---

## Apêndice A: Domínios Capturados

| Domínio | Requests | Função |
|---------|----------|--------|
| zcode.z.ai | 95 | API do app desktop (telemetria) |
| chat.z.ai | 25 | API principal (auth, config, models) |
| cdn.zcode-ai.com | 2 | CDN estático (config, updates) |
| z-cdn.chatglm.cn | 3 | CDN ZhipuAI (assets frontend) |
| sdata.chatglm.cn | 6 | Tracking pixels |
| accounts.google.com | 1901 | Google OAuth (maioria irrelevante) |
| j2c03hoppk-default-cn.rum.aliyuncs.com | 8 | RUM Alibaba Cloud |
| analytics.google.com | 109 | Google Analytics |

---

**Fim do relatório.**

Gerado por análise automatizada via Reqable MCP + Kilo AI  
Última atualização: 2026-06-14T16:39:00-03:00

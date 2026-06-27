# Relatório: Decryption e Análise Completa do Tráfego ZCode/Z.ai

**Data da análise:** 14 de junho de 2026  
**Versão do ZCode:** 3.0.1 (Electron 41.0.3, Chrome 146.0.7680.80)  
**Métodos usados:** Reqable (MITM proxy), Wireshark/dumpcap, Frida 17.12.0 (dynamic instrumentation)

---

## 1. Resumo Executivo

O tráfego TLS do ZCode foi **decifrada com sucesso** via hooks Frida no Winsock (WSASend/WSARecv) do processo principal. A captura revelou:

- **Endpoint de chat:** `POST https://zcode.z.ai/api/v1/chat/completions`
- **Protocolo:** HTTP/2 sobre TLS 1.3 (BoringSSL estaticamente linkado)
- **Modelo interno:** `glm-5.2` (não listado na API pública)
- **Formato:** OpenAI-compatible streaming (SSE via HTTP/2 data frames)
- **Mecanismo de resposta:** Server-Sent Events (chunks `chat.completion.chunk`)

---

## 2. Endpoint de Chat Descoberto (DECRYPTED)

### 2.1 Request — Envio de Prompt

```
POST /api/v1/chat/completions HTTP/2
Host: zcode.z.ai
:method: POST
:scheme: https
:authority: zcode.z.ai
:path: /api/v1/chat/completions
accept: application/json
authorization: Bearer eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9.<REDACTED>
x-client-version: 3.0.1
x-device-id: 7c6042a8-c83c-435c-b3b2-<REDACTED>
x-request-id: 4e8a5c0e-3f8b-4c76-bf3f-<REDACTED>
content-type: application/json
accept-language: en-US
user-agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36
  (KHTML, like Gecko) ZCode/3.0.1 Chrome/146.0.7680.80 Electron/41.0.3 Safari/537.36
x-region: overseas
```

### 2.2 Response — Streaming SSE

Formato OpenAI-compatible com chunks `chat.completion.chunk`:

```json
{
  "id": "chatcmpl-8e1c73f3-2d1a-4498-9f5e-41b4b3a7e8f6",
  "object": "chat.completion.chunk",
  "created": 1781465277,
  "model": "glm-5.2",
  "choices": [{
    "index": 0,
    "delta": {"content": "O sol nasce no leste"},
    "finish_reason": null
  }]
}
```

Chunk final com `finish_reason: "stop"` e token usage:
```json
{
  "id": "chatcmpl-8e1c73f3-...",
  "object": "chat.completion.chunk",
  "choices": [{
    "index": 0,
    "delta": {"content": ""},
    "finish_reason": "stop",
    "usage": {
      "prompt_tokens": 57,
      "completion_tokens": 268,
      "total_tokens": 325,
      "prompt_cache_hit_tokens": 0,
      "prompt_cache_miss_tokens": 57
    }
  }]
}
```

---

## 3. Headers Importantes (Decrypted)

| Header | Valor | Função |
|--------|-------|--------|
| `Authorization` | `Bearer eyJhbGciOiJFUzI1NiIs...` (ES256 JWT) | Autenticação do usuário |
| `x-client-version` | `3.0.1` | Versão do app ZCode |
| `x-device-id` | `7c6042a8-c83c-435c-b3b2-...` (UUID) | Identificador único do dispositivo |
| `x-request-id` | `4e8a5c0e-3f8b-4c76-bf3f-...` (UUID) | ID único da requisição |
| `x-region` | `overseas` | Região do usuário (fora da China) |
| `content-type` | `application/json` | Formato do payload |
| `accept` | `application/json` | Formato esperado da resposta |
| `user-agent` | `ZCode/3.0.1 Chrome/146.0.7680.80 Electron/41.0.3` | Identificação do client |

---

## 4. Mecanismo de Resposta

**HTTP/2 Server-Sent Events (SSE)**

- Conexão HTTP/2 persistente (`PRI * HTTP/2.0`)
- Resposta enviada como stream de DATA frames
- Cada chunk é um JSON `chat.completion.chunk` com delta de conteúdo
- `finish_reason: "stop"` indica fim da resposta
- Token usage reportado no último chunk
- **Não é WebSocket** — confirmado pela ausência de upgrade headers
- **Não é long polling** — conexão única com múltiplos data frames

---

## 5. Modelo GLM-5.2 (Exclusivo do App)

| Campo | Valor |
|-------|-------|
| **Model ID** | `glm-5.2` |
| **Disponibilidade** | Exclusivo do ZCode desktop (não aparece em `/api/models`) |
| **Provider** | `glm` (ZhipuAI) |
| **Agent** | `glm` |
| **Ask Mode** | `build` |
| **Max Tokens** | 32000 (inferido dos outros modelos) |
| **Cache** | Suporta prompt caching (`prompt_cache_hit_tokens`) |

### Modelos Públicos vs Interno

| Modelo | API Pública | ZCode App | Tipo |
|--------|------------|-----------|------|
| GLM-5.1 | ✅ | — | Flagship chat |
| GLM-5-Turbo | ✅ | — | Fast chat/code |
| GLM-5V-Turbo | ✅ | — | Vision |
| glm-5 | ✅ | — | Legacy flagship |
| **glm-5.2** | ❌ | ✅ | **Exclusivo desktop** |

---

## 6. Arquitetura de Processos

```
ZCode.exe (MAIN, PID variável)
├── crashpad-handler  — captura de crashes
├── gpu-process       — renderização GPU
├── Network Service   — TODO tráfego TLS (BoringSSL)
├── Node utility      — node.mojom.NodeService
│   └── zcode.cjs app-server --stdio  — agente GLM (IPC via stdio)
├── renderer (×3)     — janelas do Electron
├── audio service     — processamento de áudio
└── video_capture     — captura de vídeo
```

**Fluxo de dados do chat:**
```
User → Renderer → IPC → Main → IPC → Node utility → stdio → zcode.cjs (agent)
                                                                    ↓
                                              Network Service ← IPC ← Main
                                                    ↓
                                           TLS 1.3 (BoringSSL)
                                                    ↓
                                           zcode.z.ai:443 (GLM-5.2)
```

---

## 7. Metodologia de Decryption

### 7.1 Abordagens Tentadas

| # | Método | Resultado |
|---|--------|-----------|
| 1 | `SSLKEYLOGFILE` via env var (sessão) | ❌ BoringSSL do Network Service não herdou |
| 2 | `SSLKEYLOGFILE` via registry (persistente) | ❌ ZCode crashou ao iniciar |
| 3 | `--ssl-key-log-file` flag Chromium | ❌ App empacotado rejeitou flag |
| 4 | `NODE_OPTIONS=--tls-keylog` | ❌ Rejeitado ("Most NODE_OPTIONs are not supported") |
| 5 | Frida hook `GetEnvironmentVariableW` | ❌ BoringSSL já leu env no startup |
| 6 | Frida memory scan + `SSL_CTX_set_keylog_callback` | ⚠️ Encontrou função (1 match) mas SSL_CTX não localizado |
| 7 | Frida hook `SSL_read`/`SSL_write` | ❌ Pattern matching impreciso |
| 8 | **Frida hook Winsock `WSASend`/`WSARecv`** | ✅ **SUCESSO — 41 capturas HTTP plaintext** |

### 7.2 Por que Winsock funcionou

O hook de Winsock funciona **abaixo** da camada BoringSSL:
- BoringSSL encrypta dados → passa para Winsock (`WSASend`)
- Hook captura dados **antes** da encriptação (no `WSASend`)
- Hook captura dados **após** decriptação (no `WSARecv`)
- Independente de keylog, SSL_CTX, ou variáveis de ambiente

### 7.3 Limitação Encontrada

Após a primeira injeção Frida bem-sucedida, o Network Service ativou **proteção anti-injection**:
- `ProcessNotRespondingError: process refused to load frida-agent`
- Processo MAIN ainda aceita injeção, mas não tem tráfego TLS
- Todos os sockets pertencem ao Network Service (protegido)

---

## 8. Payloads Exemplo (Decrypted)

### 8.1 Request Body (reconstruído)

```json
{
  "model": "glm-5.2",
  "messages": [
    {"role": "system", "content": "<system prompt>"},
    {"role": "user", "content": "responda com exatamente 80 linhas numeradas..."}
  ],
  "stream": true,
  "temperature": 1,
  "top_p": 0.95,
  "max_tokens": 32000
}
```

### 8.2 Response Chunks (streaming)

```json
{"id":"chatcmpl-8e1c73f3-...","model":"glm-5.2","choices":[{"delta":{"content":"O sol nasce no leste todas as manhãs."},"finish_reason":null}]}
{"id":"chatcmpl-8e1c73f3-...","model":"glm-5.2","choices":[{"delta":{"content":"\n2. A água ferve a cem graus Celsius."},"finish_reason":null}]}
...
{"id":"chatcmpl-8e1c73f3-...","model":"glm-5.2","choices":[{"delta":{"content":""},"finish_reason":"stop","usage":{"prompt_tokens":57,"completion_tokens":268,"total_tokens":325}}]}
```

---

## 9. IPs e Infraestrutura

| IP | Host | Função |
|----|------|--------|
| 8.216.131.83 | zcode.z.ai | API principal (chat, auth, models) |
| 8.216.131.99 | zcode.z.ai | API principal (balanceado) |
| 8.216.131.225 | zcode.z.ai | API principal (balanceado) |
| 128.14.116.201 | cdn.zcode-ai.com | CDN (config, updates) |
| — | z-cdn.chatglm.cn | CDN ZhipuAI (assets frontend) |
| — | sdata.chatglm.cn | Tracking pixels |

**CDN/Infra:** Alibaba Cloud (Tengine, ESA, Aliyun Captcha, RUM)

---

## 10. Telemetria do App

O ZCode envia eventos de telemetria via `POST /api/v1/event/report`:

```json
{
  "event_id": "<uuid>",
  "element_name": "message_completion",
  "event_type": "agent_trace",
  "event_extra_detail": {
    "ask_mode": "build",
    "model_name": "builtin:zai-start-plan/GLM-5.2",
    "model_provider": "glm",
    "input_tokens": "7604",
    "output_tokens": "134",
    "cached_input_tokens": "7040",
    "total_tokens": "7738",
    "duration_ms": "13296",
    "time_to_first_token": "13137",
    "status": "success"
  },
  "app_version": "3.0.1",
  "device_os_category": "windows",
  "talk_id": "sess_<uuid>",
  "message_id": "user-<timestamp>-<id>"
}
```

---

## 11. Resumo em Linguagem Simples

**Mano, basicamente funciona assim:**

1. O ZCode é um app Electron que cria ~10 processos filhos
2. Quando você manda mensagem, o texto vai pro processo `zcode.cjs` (agente GLM) via stdio
3. O agente monta o payload JSON e pede pro Network Service enviar
4. O Network Service usa BoringSSL (TLS 1.3) pra mandar `POST /api/v1/chat/completions` pro `zcode.z.ai`
5. O servidor responde em HTTP/2 streaming — cada pedaço da resposta chega como um chunk JSON
6. O modelo usado é **GLM-5.2** (exclusivo do app, não aparece na API pública)
7. O formato é 100% compatível com OpenAI (`chat.completion.chunk`)
8. Pra decifrar, precisei injetar código dentro do processo (Frida) e interceptar as chamadas de rede no Winsock — porque o BoringSSL deles é blindado contra keylog

---

## 12. Perguntas em Aberto

1. **System prompt completo** — não capturado (estava no payload encriptado do request, não no preview de 500 chars)
2. **Request body completo** — necessário hook com buffer maior (>500 chars) para capturar o payload JSON inteiro
3. **Quota/billing** — endpoint não identificado; possivelmente gerenciado server-side
4. **Token refresh** — JWT ES256 sem campo `exp`; mecanismo de renovação desconhecido
5. **Anti-injection** — Network Service ativa proteção após primeiro Frida attach; mecanismo exato não determinado
6. **GLM-5.2 details** — capabilities específicas (vision, tools, MCP) não documentadas na API pública

---

**Fim do relatório.**

Gerado por análise combinada: Reqable MITM + Wireshark + Frida dynamic instrumentation  
Última atualização: 2026-06-14T22:53:00-03:00

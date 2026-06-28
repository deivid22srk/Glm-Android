# glm5.2proxy

Proxy local em Go para usar contas Z.ai/ZCode Start Plan de dois jeitos:

- como um gerenciador de contas para o proprio app ZCode, com troca de conta e refresh live dentro do cliente deles;
- como uma API OpenAI-compatible local para usar em Kilo Code, Roo Code, Open WebUI, scripts e outros clientes externos.

O projeto tem duas superficies:

- app desktop Wails com painel React para contas, cotas, fila, logs, porta, API keys e integracao com ZCode;
- servidor headless para rodar apenas a API/proxy.

## Novo sistema de integracao com o ZCode

O projeto agora consegue sincronizar as contas salvas no proxy diretamente para
o ambiente interno do ZCode.

Na pratica:

- voce adiciona e organiza as contas no painel do `glm5.2proxy`;
- quando ativa uma conta no proxy, ele tambem pode aplicar essa mesma conta no ZCode;
- se o bridge estiver disponivel, o ZCode recarrega a propria janela e passa a usar a conta nova sem voce precisar trocar manualmente no app deles;
- o painel mostra os eventos dessa sincronizacao e dos refreshes live.

Isso resolve o problema de manter dois pools separados:

- um pool de contas no proxy para clientes OpenAI-compatible;
- e outro estado manual dentro do ZCode.

Agora o proxy pode ser a origem de verdade das contas, e o ZCode pode seguir o
que foi ativado nele.

## Modos de operacao

Hoje o projeto tem dois caminhos de operacao:

- usar a API OpenAI-compatible local;
- usar o app para trocar a conta diretamente no ZCode.

Use o modo OpenAI-compatible quando quiser integrar clientes externos como Roo
Code, Kilo Code, Open WebUI e scripts. Use `Aplicar no ZCode` quando quiser
trocar a conta dentro do app ZCode instalado na maquina.

## Dois modos de uso

### 1. Usar o proxy como API OpenAI-compatible

Nesse modo, o app expoe:

- `POST /v1/chat/completions`
- `GET /v1/models`

Ele traduz as chamadas para o formato usado pelo ZCode/GLM e faz rotacao por
conta/modelo quando a cota esgota ou quando a conta atual deixa de ser viavel
para a request.

Esse modo serve para:

- Kilo Code
- Roo Code
- Open WebUI
- apps/scripts que falam com API estilo OpenAI

### 2. Usar o proxy para rotacionar contas diretamente no app ZCode

Nesse modo, o proxy nao serve apenas como API externa. Ele tambem conversa com
o ambiente local do ZCode instalado na maquina.

Quando voce usa `Aplicar no ZCode`:

- o proxy grava a conta nos arquivos internos do ZCode;
- garante que o bridge do renderer esteja instalado;
- enfileira um refresh live;
- o ZCode recarrega a janela e atualiza o perfil visual e o runtime da conta.

Em outras palavras: o proxy pode comandar qual conta o ZCode vai usar.

## Diferenca entre os botoes do app

O painel agora deixa isso claro tambem por hover/tooltip nos botoes.

### `Usar agora`

Esse botao:

- troca a conta ativa do proxy;
- atualiza a conta que o modo OpenAI-compatible vai usar como base;
- nao grava nada no ambiente interno do ZCode.

Esse e o botao certo quando voce quer mudar somente o proxy/API local.

### `Aplicar no ZCode`

Esse botao:

- grava a conta diretamente no ambiente interno do ZCode;
- nao muda a conta ativa do proxy;
- serve para forcar a conta desejada no cliente ZCode sem alterar o pool
  principal do proxy.

Esse e o botao certo quando voce quer comandar so o ZCode.

## Manual de uso

### Fluxo recomendado para uso no proprio ZCode

1. Abra o `glm5.2proxy.exe`.
2. Adicione uma ou mais contas no painel.
3. Organize a fila na ordem desejada.
4. Passe o mouse nos botoes para ver a explicacao de cada um.
5. Se quiser trocar a conta ativa do proxy/API, use `Usar agora`.
6. Se quiser trocar a conta dentro do app ZCode, use `Aplicar no ZCode`.
7. Aguarde o refresh live ou o pequeno reload do ZCode.
8. Confirme no ZCode se o perfil/modelo esperado apareceu.

### Fluxo recomendado para rotacao manual

1. Mantenha varias contas salvas no painel.
2. Quando uma conta ficar ruim, limitada ou sem plano util no ZCode, selecione
   outra.
3. Use `Aplicar no ZCode` para empurrar essa conta para dentro do ZCode.
4. Continue trabalhando no ZCode com a conta nova.

### Fluxo para clientes OpenAI-compatible

1. Inicie o app.
2. Adicione pelo menos uma conta.
3. Gere uma API key local no painel.
4. Configure o cliente externo para usar `http://127.0.0.1:3005/v1`.
5. Use `glm-5.2` ou `glm-5-turbo`.

## Como funciona o bridge do ZCode

O ZCode nao expoe publicamente um endpoint HTTP para trocar a conta live. O
refresh interno fica atras de um RPC privado via Electron `MessagePort`.

Por causa disso, o `glm5.2proxy` instala um bridge pequeno no renderer do
ZCode.

Esse bridge:

- faz polling em `GET /api/admin/zcode/bridge/next`;
- recebe comandos de refresh enfileirados pelo proxy;
- chama internamente `modelProviderService.refreshCodingPlanApiKey(...)`;
- confirma o resultado em `POST /api/admin/zcode/bridge/ack`;
- recarrega a janela do ZCode para o perfil visual sair do estado antigo.

### O que e injetado

O projeto inclui o script:

```text
scripts/patch-zcode-live-refresh.ps1
```

Esse script:

- localiza o `app.asar` do ZCode;
- cria backup automatico do arquivo original;
- extrai o bundle do renderer;
- injeta o snippet do bridge;
- repacka o `app.asar`;
- permite restaurar a partir do backup.

### Isso e automatico?

Sim. No fluxo novo, o proprio app tenta fazer isso automaticamente quando:

- detecta que o ZCode esta instalado; e
- precisa aplicar uma conta no ZCode; e
- o bridge `v2` ainda nao esta instalado.

Se necessario, ele reinicia o ZCode uma vez para carregar o bridge novo. Depois
disso, as proximas trocas podem acontecer pelo fluxo live.

### Transparencia e seguranca

Esse comportamento altera arquivos locais do ZCode instalado na maquina. Esse
funcionamento faz parte do projeto e e documentado de forma explicita:

- o patch e local, nao remoto;
- um backup do `app.asar` e criado automaticamente antes de qualquer alteracao;
- o projeto nao recomenda desativar antivirus;
- se houver falso positivo, a recomendacao e adicionar excecao apenas para a
  pasta ou executavel confiavel do projeto.

Alguns antivirus podem estranhar esse comportamento porque ele modifica o
ambiente local de outro aplicativo Electron. A orientacao do projeto nao e
desligar a protecao do sistema, e sim tratar eventuais falsos positivos de
forma pontual e consciente.

## Por que aparece tanto PowerShell

No Windows, parte importante da integracao com o ZCode depende de automacao do
sistema local. O projeto usa PowerShell porque ele e o jeito mais direto e
confiavel de fazer isso no Windows.

O PowerShell entra principalmente para:

- localizar a instalacao do ZCode;
- inspecionar processos em execucao;
- aplicar ou restaurar o patch do `app.asar`;
- copiar backups;
- reiniciar o ZCode quando necessario;
- interagir com arquivos de configuracao locais.

Entao, ver PowerShell nesse projeto nao significa improviso. Significa que a
aplicacao desktop esta fazendo integracao real com um app Electron externo
instalado localmente.

Ainda assim, essa parte pode e deve ser refinada. Ha espaco para:

- menos passos externos;
- mensagens de estado mais claras;
- menos dependencia de reinicio;
- mais resiliencia em cenarios de corrida e refresh.

Mas para colocar a funcao em operacao agora, esse desenho ja entrega valor.

## Processo interno resumido

Quando voce manda trocar a conta no ZCode pelo `glm5.2proxy`, o fluxo interno,
em alto nivel, e este:

1. o painel React envia o comando para o backend Go;
2. o backend localiza a conta salva;
3. o backend escreve credenciais e configuracao no ambiente do ZCode;
4. ele valida/repara o estado de `coding plan` local;
5. se o bridge estiver disponivel, enfileira um refresh live;
6. o renderer do ZCode busca esse comando;
7. o ZCode atualiza o provider/model provider em memoria;
8. a janela recarrega para refletir o estado novo;
9. o app volta a mostrar a conta/modelo corretos.

Se o bridge ainda nao estiver presente, o projeto tenta instalar o patch local
e, se preciso, reinicia o ZCode uma vez para entrar no modo live nas proximas
trocas.

## Funcionalidades

- Painel desktop Wails/React em `cmd/desktop/frontend`.
- Login OAuth ZCode pelo backend.
- Cofre local AES-256-GCM para contas e tokens.
- API keys locais para proteger `/v1/*`.
- Liga/desliga da API OpenAI-compatible sem fechar o painel.
- Reordenacao de contas e ativacao manual.
- Consulta de cota por conta/modelo.
- Rotacao automatica quando a conta ativa esgota.
- Fila local por `conta + modelo` para evitar limite de concorrencia upstream.
- Streaming e non-streaming.
- Tools/function calling, system/developer messages e resultados de tools.
- Captcha headless com Chrome/Edge e fallback interativo.
- Logs administrativos no painel e em `/api/admin/logs`.

## Arquitetura

```text
Cliente OpenAI-compatible
  -> http://127.0.0.1:3005/v1
  -> Go HTTP/SSE proxy
  -> Tradutor OpenAI Chat Completions
  -> ZCode/GLM upstream

App desktop Wails
  -> Painel React
  -> Go bindings + /api/admin/*
  -> Contas, OAuth, cota, fila, API keys, logs e settings
```

O runtime Node antigo nao e mais necessario para o proxy. Node/npm ficam apenas
no frontend do app desktop.

## Executar

Servidor headless:

```powershell
go run ./cmd/server
```

Ou pelo script npm da raiz:

```powershell
npm start
```

App desktop em desenvolvimento:

```powershell
npm run desktop:dev
```

Porta padrao:

```text
http://127.0.0.1:3005
```

## Build

Validacao recomendada:

```powershell
go test ./...
go vet ./...
npm run build --prefix .\cmd\desktop\frontend
```

### Resumo rapido

- Quer **so a API/proxy**? Build do `cmd/server`.
- Quer **o app com interface desktop Wails**? Build do app desktop.
- **Linux headless/server** pode ser compilado a partir do Windows.
- **Linux desktop Wails** deve ser compilado em ambiente Linux real ou WSL2 com
  as dependencias graficas instaladas. Nao conte com Windows puro para isso.

### Windows

Servidor Windows:

```powershell
npm run build
.\build\glm5.2proxy-server.exe
```

App desktop Windows:

```powershell
npm run desktop:build
```

Saida principal:

```text
build\bin\glm5.2proxy.exe
```

### Linux server/headless compilado no Windows

Se o objetivo for rodar apenas a API OpenAI-compatible sem interface grafica,
este comando gera um binario Linux amd64 diretamente da maquina Windows:

```powershell
$env:CGO_ENABLED='0'
$env:GOOS='linux'
$env:GOARCH='amd64'
go build -trimpath -ldflags='-s -w' `
  -o build\glm5.2proxy-server-linux-amd64 ./cmd/server
Remove-Item Env:GOOS,Env:GOARCH,Env:CGO_ENABLED -ErrorAction SilentlyContinue
```

Depois, no Linux:

```bash
chmod +x ./glm5.2proxy-server-linux-amd64
./glm5.2proxy-server-linux-amd64
```

### Linux desktop Wails

O app desktop Linux **nao deve ser buildado no Windows puro**. O caminho
recomendado e buildar em:

- Linux nativo; ou
- WSL2/Ubuntu; ou
- CI Linux.

Exemplo em Ubuntu/WSL2:

```bash
sudo apt update
sudo apt install -y build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev
go test ./...
npm install
npm run desktop:build
```

Saida esperada do desktop Linux:

```text
build/bin/glm5.2proxy
```

Quem baixar este repositorio no Linux ja tera o codigo mais recente. Se ainda
nao houver release pronta para o sistema desejado, basta compilar o binario
Linux localmente.

## Release Local

Os artefatos gerados ficam em:

```text
build\glm5.2proxy-server.exe
build\glm5.2proxy-server-windows-amd64.exe
build\glm5.2proxy-server-linux-amd64
build\bin\glm5.2proxy.exe
release-dist\glm5.2proxy-windows-desktop-amd64.zip
release-dist\glm5.2proxy-linux-desktop-amd64.tar.gz
release-dist\glm5.2proxy-linux-desktop-amd64.deb
release-dist\glm5.2proxy-windows-server-amd64.zip
release-dist\glm5.2proxy-linux-server-amd64.zip
release-dist\SHA256SUMS.txt
```

`release-dist/`, `cmd/desktop/frontend/dist/` e `node_modules/` sao artefatos
locais normalmente ignorados pelo Git. Alguns binarios dentro de `build/` podem
ser versionados manualmente quando o repositorio publica uma build pronta.

## Uso no Windows

1. Extraia `glm5.2proxy-windows-desktop-amd64.zip` em uma pasta fixa.
2. Execute `glm5.2proxy.exe`.
3. No painel, adicione uma conta ZCode.
4. Crie uma API key no painel.
5. Configure seu cliente OpenAI-compatible:

```text
Base URL: http://127.0.0.1:3005/v1
Model: glm-5.2 ou glm-5-turbo
API key: chave criada no painel
```

Dados locais:

```text
C:\Users\<usuario>\.glm5.2proxy
```

Essa pasta contem contas, tokens e configuracoes locais. Nao envie junto com
release ou commit.

## Uso no Linux

Se voce esta no Linux e **nao existe binario pronto para o seu sistema** no
repositorio/release, faca assim:

1. Baixe o repositorio atual.
2. Escolha se quer `server` ou `desktop`.
3. Para `server`, compile o `cmd/server`.
4. Para `desktop`, compile em Linux/WSL2, nao em Windows puro.
5. Rode o binario gerado e configure a conta/API key pelo painel ou pela API.

Em outras palavras: o repositorio ja contem o codigo novo; o que pode faltar e
somente o artefato Linux pronto.

## Modelos

| Modelo publico | Upstream | Cota diaria observada |
|---|---|---:|
| `glm-5.2` | `GLM-5.2` | 3.000.000 tokens |
| `glm-5-turbo` | `GLM-5-Turbo` | 2.000.000 tokens |

Capacidades:

```text
GET /api/admin/models/capabilities
GET /v1/models
```

## API OpenAI-Compatible

```text
POST /v1/chat/completions
POST /chat/completions
GET  /v1/models
```

Enquanto nenhuma API key estiver cadastrada, o acesso local pode funcionar sem
segredo. Depois da primeira chave, envie:

```text
Authorization: Bearer zkp_...
```

## APIs Administrativas

Contas e fila:

```text
GET    /api/admin/accounts
GET    /api/admin/accounts/{id}
POST   /api/admin/accounts/{id}/activate
PUT    /api/admin/accounts/order
DELETE /api/admin/accounts/{id}
GET    /api/admin/queue
```

Login:

```text
POST /api/admin/auth/login/start
GET  /api/admin/auth/login/poll?flow_id=...
```

Settings e API keys:

```text
GET    /api/admin/settings
PATCH  /api/admin/settings
GET    /api/admin/api-keys
POST   /api/admin/api-keys
DELETE /api/admin/api-keys/{id}
```

Thinking:

```text
GET /api/admin/thinking
PUT /api/admin/thinking
GET    /api/admin/accounts/{id}/thinking
PUT    /api/admin/accounts/{id}/thinking
DELETE /api/admin/accounts/{id}/thinking
```

Logs:

```text
GET /api/admin/logs?limit=200
```

Captcha:

```text
GET  /zcode/captcha/config
GET  /zcode/captcha/browser
GET  /zcode/captcha/poll
POST /zcode/captcha/submit
POST /zcode/captcha/test
```

Documentacao detalhada da API: [docs/API.md](docs/API.md).

## Estrutura

```text
cmd/server                  servidor headless
main.go                     app desktop Wails
cmd/desktop/frontend        painel React do app desktop
internal/accounts           cofre e contas
internal/accountpool        fila e rotacao
internal/api                APIs HTTP/admin/OpenAI-compatible
internal/app                composicao e lifecycle
internal/auth               OAuth ZCode
internal/captcha            bridge, pagina e navegador
internal/config             configuracao
internal/models             modelos e capacidades
internal/openai             traducao de formatos
internal/proxy              transporte e SSE
internal/quota              billing e porcentagens
internal/requestqueue       serializacao por conta/modelo
internal/state              porta, API keys e thinking
internal/upstream           credenciais e model-io fallback
tests                       testes Go
```

## Testes

```powershell
go test ./...
go vet ./...
npm run build --prefix .\cmd\desktop\frontend
```

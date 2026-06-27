# Investigacao: Captcha Standalone do ZCode

Data: 15 de junho de 2026

## Conclusao

O ZCode nao usa hCaptcha. Ele usa o SDK oficial Aliyun Captcha 2.0 V3 para
Web/H5. O proxy nao precisa manter o ZCode aberto: uma pagina local executada
automaticamente em um Chrome/Edge headless pode executar o mesmo fluxo oficial e
fornecer uma prova fresca para cada request.

O fluxo standalone foi validado com uma chamada real ao `GLM-5-Turbo`, que
respondeu `standalone-ok`. O bridge registrou `lastClient: standalone-browser`.

O modo headless automatico tambem foi validado com uma chamada real ao
`GLM-5-Turbo`, que respondeu `headless-ok`. O processo foi encerrado durante o
teste e reiniciado automaticamente com um novo PID antes da chamada.

Isso nao remove o captcha nem cria provas falsas. Substitui o renderer Electron
do ZCode por uma pagina local que usa o SDK oficial. Quando a politica Aliyun
exigir desafio interativo, o usuario precisa resolve-lo no navegador.

## Fluxo confirmado no app

```text
getCaptchaConfig()
  -> GET /api/v1/client/configs
  -> region + prefix + sceneId
  -> carrega AliyunCaptcha.js
  -> initAliyunCaptcha(...)
  -> startTracelessVerification()
  -> success(captchaVerifyParam)
  -> X-Aliyun-Captcha-Verify-Param
  -> request ao modelo
```

Configuracao verificada ao vivo:

```text
enabled: true
region: sgp
prefix: no8xfe
sceneId: 11xygtvd
```

Endpoint publico:

```text
GET https://zcode.z.ai/api/v1/client/configs?app_version=3.0.1&platform=win32-x64
```

SDK carregado dinamicamente:

```text
https://o.alicdn.com/captcha-frontend/aliyunCaptcha/AliyunCaptcha.js
```

## Comportamento encontrado no bundle

- O SDK exige `window` e `document`; Node puro nao executa o fluxo oficial.
- O app espera 2 segundos depois do carregamento para coleta de sinais.
- O app tenta verificacao sem interacao primeiro.
- Quando necessario, chama `show()` e apresenta desafio interativo.
- As verificacoes sao serializadas em uma fila.
- Timeout usado pelo app: 120 segundos.
- A configuracao do captcha fica em cache por 60 segundos.
- O app detecta `F008` como prova reutilizada/submissao duplicada.
- Cada envio pede uma prova nova; replay de prova ja usada retorna `3007`.

## Formato da prova

O valor recebido do SDK e enviado diretamente no header. No teste standalone,
ele era Base64 de um JSON com:

```text
certifyId
sceneId
isSign
securityToken
```

O `securityToken` e assinado/gerado pelo SDK e pela infraestrutura Aliyun. Nao
ha algoritmo local simples ou segredo no app que permita fabricar esse valor.

## Alternativas avaliadas

### Reutilizar header capturado

Nao funciona. A prova e descartavel e o backend retorna `3007` depois do uso.

### Gerar em Node puro

Nao funciona pelo fluxo oficial. O SDK exige navegador e coleta informacoes de
ambiente/dispositivo.

### Copiar o JavaScript do CDN para o projeto

Nao recomendado. A documentacao oficial exige carregamento dinamico para manter
atualizacoes e capacidade de seguranca.

### Remover o header

Nao funciona para `/zcode-plan/anthropic/v1/messages`; o backend exige a
verificacao.

### Broker standalone em navegador

Funciona e foi implementado. Ele usa configuracao publica, SDK oficial,
verificacao sem interacao e fallback interativo real.

### Broker headless automatico

Funciona e foi implementado como modo padrao. O proxy inicia Chrome, ou Edge
como fallback, com perfil isolado e sem janela visivel. Isso evita baixar um
Chromium adicional. Se o processo encerrar, o proxy o inicia novamente.

O headless nao tenta contornar desafios humanos. Quando a Aliyun exigir
interacao, a request informa o bloqueio e o usuario pode executar
`npm run captcha:visible`.

Foram comparados Edge, Chrome e o Chromium Headless Shell ja presentes na
maquina. Edge e Chrome passaram na verificacao sem interacao; Chrome abriu menos
servicos adicionais e foi escolhido como primeira opcao. O Headless Shell foi
recusado para verificacao sem interacao e exigiu desafio humano, portanto nao e
usado automaticamente.

## Implementacao no proxy

```text
GET  /zcode/captcha/browser  pagina standalone
GET  /zcode/captcha/config   configuracao publica sanitizada
POST /zcode/captcha/test     testa geracao sem expor a prova
```

Executar:

```powershell
npm start
```

O proxy inicia e prioriza `headless-browser` automaticamente. Para um desafio
interativo:

```powershell
npm run captcha:visible
```

## Limites restantes

- Ainda existe dependencia de um ambiente browser.
- A Aliyun pode exigir interacao humana dependendo da avaliacao de risco.
- A prova deve ser gerada novamente para cada request.
- Mudancas futuras no SDK ou na configuracao do ZCode podem exigir ajuste.

## Fontes oficiais

- Alibaba Cloud: integracao Web/H5 do Captcha 2.0 V3:
  https://help.aliyun.com/zh/captcha/captcha2-0/user-guide/new-architecture-for-web-and-h5-client-access
- Alibaba Cloud, versao internacional da mesma documentacao:
  https://www.alibabacloud.com/help/zh/captcha/captcha2-0/user-guide/new-architecture-for-web-and-h5-client-access

## Veredito

Nao foi encontrado um modo legitimo de eliminar a verificacao do backend ou de
fabricar a prova em Node puro. A solucao independente correta e substituir o
Electron do ZCode por um broker de navegador que executa o SDK oficial e
preserva o desafio humano quando a Aliyun o exigir.

package captcha

const BrowserPage = `<!doctype html>
<html lang="pt-BR">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>ZCode Captcha Broker</title>
  <style>
    :root { color-scheme: dark; font-family: system-ui, sans-serif; }
    body { margin: 0; background: #111; color: #eee; }
    main { max-width: 680px; margin: 40px auto; padding: 24px; }
    #status { padding: 12px; border: 1px solid #333; border-radius: 6px; background: #181818; }
    #captcha-element { min-height: 1px; }
    #captcha-button { position: fixed; left: 50%; top: 50%; width: 1px; height: 1px; opacity: 0; }
  </style>
</head>
<body>
  <main>
    <h1>ZCode Captcha Broker</h1>
    <p>Mantenha esta aba aberta somente se um desafio interativo for exigido.</p>
    <div id="status">Inicializando...</div>
    <div id="captcha-element"></div>
    <button id="captcha-button" type="button" aria-hidden="true"></button>
  </main>
  <script>
    const statusElement = document.getElementById("status");
    const captchaElement = document.getElementById("captcha-element");
    const captchaButton = document.getElementById("captcha-button");
    const client = new URLSearchParams(location.search).get("client") || "standalone-browser";
    const headless = client === "headless-browser";
    let config, sdkLoadedAt = 0;
    function setStatus(message) {
      statusElement.textContent = message;
      document.title = message.includes("interativo") ? "Acao necessaria - ZCode Captcha" : "ZCode Captcha Broker";
    }
    async function loadConfig() {
      const response = await fetch("/zcode/captcha/config", { cache: "no-store" });
      const body = await response.json();
      if (!response.ok) throw new Error(body.error?.message || "Falha ao carregar configuracao");
      config = body;
      window.AliyunCaptchaConfig = { region: config.region, prefix: config.prefix };
    }
    async function loadSdk() {
      if (typeof window.initAliyunCaptcha === "function") return;
      await new Promise((resolve, reject) => {
        const script = document.createElement("script");
        script.src = "https://o.alicdn.com/captcha-frontend/aliyunCaptcha/AliyunCaptcha.js";
        script.async = true;
        script.onload = () => { sdkLoadedAt = Date.now(); resolve(); };
        script.onerror = () => reject(new Error("Falha ao carregar SDK Aliyun"));
        document.head.appendChild(script);
      });
    }
    function submit(request, payload) {
      return fetch("/zcode/captcha/submit", {
        method: "POST", headers: { "content-type": "application/json" },
        body: JSON.stringify({ id: request.id, client, ...payload }),
      });
    }
    async function verify(request) {
      captchaElement.replaceChildren();
      setStatus("Preparando verificacao oficial...");
      let settled = false, instance, timeout, resolveCompletion;
      const completion = new Promise((resolve) => { resolveCompletion = resolve; });
      const finish = async (payload) => {
        if (settled) return;
        settled = true; clearTimeout(timeout);
        await submit(request, payload);
        setStatus(payload.token ? "Verificacao concluida. Aguardando request..." : "Verificacao falhou. Aguardando nova tentativa...");
        resolveCompletion();
      };
      await new Promise((resolve, reject) => {
        window.initAliyunCaptcha({
          SceneId: config.sceneId, mode: "popup",
          language: navigator.language.toLowerCase().startsWith("zh") ? "cn" : "en",
          showErrorTip: false, delayBeforeSuccess: false,
          element: "#captcha-element", button: "#captcha-button",
          getInstance(value) { instance = value; resolve(); },
          success(token) { void finish({ token, region: config.region }); },
          fail(result) {
            const token = result?.captchaVerifyParam || result?.CaptchaVerifyParam;
            if (token) return void finish({ token, region: config.region });
            if (result?.verifyResult === false && instance?.show) {
              if (headless) return void finish({ error: "Aliyun exigiu desafio interativo. Abra /zcode/captcha/browser?client=standalone-browser." });
              setStatus("Desafio interativo exigido. Resolva a verificacao exibida.");
              instance.show(); return;
            }
            if (result?.verifyCode === "F008") return void finish({ error: "Captcha rejeitado por reutilizacao." });
          },
          onError(error) { reject(error instanceof Error ? error : new Error(error?.message || "Captcha SDK error")); },
        });
      });
      const waitMs = Math.max(0, 2000 - (Date.now() - sdkLoadedAt));
      if (waitMs) await new Promise((resolve) => setTimeout(resolve, waitMs));
      timeout = setTimeout(() => void finish({ error: "Captcha timed out." }), request.timeoutMs || 120000);
      setStatus("Executando verificacao sem interacao...");
      if (typeof instance?.startTracelessVerification === "function") instance.startTracelessVerification();
      else if (headless) void finish({ error: "Captcha interativo necessario." });
      else if (typeof instance?.show === "function") instance.show();
      else captchaButton.click();
      await completion;
    }
    async function poll() {
      for (;;) {
        try {
          const response = await fetch("/zcode/captcha/poll?client=" + encodeURIComponent(client), { cache: "no-store" });
          if (response.status === 200) await verify(await response.json());
          else setStatus("Broker pronto. Aguardando request...");
        } catch (error) {
          setStatus("Erro: " + (error?.message || String(error)));
          await new Promise((resolve) => setTimeout(resolve, 2000));
        }
      }
    }
    (async () => { await loadConfig(); await loadSdk(); setStatus("Broker pronto. Aguardando request..."); await poll(); })()
      .catch((error) => setStatus("Erro fatal: " + (error?.message || String(error))));
  </script>
</body>
</html>`

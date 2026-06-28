param(
  [string]$ZCodeAsarPath = "$env:LOCALAPPDATA\Programs\ZCode\resources\app.asar",
  [string]$ProxyBaseUrl = "http://127.0.0.1:3005",
  [switch]$Restore,
  [switch]$ForceKill
)

$ErrorActionPreference = "Stop"

function Stop-ZCodeIfNeeded {
  $processes = Get-Process -Name "ZCode" -ErrorAction SilentlyContinue
  if (-not $processes) { return }
  if (-not $ForceKill) {
    $pids = ($processes | Select-Object -ExpandProperty Id) -join ", "
    throw "ZCode esta rodando (PID: $pids). Feche o ZCode ou rode com -ForceKill."
  }
  $processes | Stop-Process -Force
  Start-Sleep -Milliseconds 800
}

function Get-LatestBackup([string]$Path) {
  $dir = Split-Path -Parent $Path
  $name = Split-Path -Leaf $Path
  Get-ChildItem -Path $dir -Filter "$name.glm5proxy-backup-*" -File |
    Sort-Object LastWriteTime -Descending |
    Select-Object -First 1
}

if (-not (Test-Path -LiteralPath $ZCodeAsarPath)) {
  throw "app.asar nao encontrado em: $ZCodeAsarPath"
}

Stop-ZCodeIfNeeded

if ($Restore) {
  $backup = Get-LatestBackup $ZCodeAsarPath
  if (-not $backup) {
    throw "Nenhum backup encontrado para restaurar."
  }
  Copy-Item -LiteralPath $backup.FullName -Destination $ZCodeAsarPath -Force
  Write-Host "Restaurado: $($backup.FullName) -> $ZCodeAsarPath"
  exit 0
}

$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
$backupPath = "$ZCodeAsarPath.glm5proxy-backup-$timestamp"
Copy-Item -LiteralPath $ZCodeAsarPath -Destination $backupPath -Force

$workDir = Join-Path $env:TEMP "zcode-live-refresh-patch-$timestamp"
if (Test-Path -LiteralPath $workDir) {
  Remove-Item -LiteralPath $workDir -Recurse -Force
}
New-Item -ItemType Directory -Path $workDir | Out-Null

npx --yes @electron/asar extract "$ZCodeAsarPath" "$workDir"

$rendererDir = Join-Path $workDir "out\renderer\assets"
$renderer = Get-ChildItem -Path $rendererDir -Filter "index-*.js" -File |
  Sort-Object Length -Descending |
  Select-Object -First 1
if (-not $renderer) {
  throw "Bundle renderer index-*.js nao encontrado em $rendererDir"
}

$source = Get-Content -LiteralPath $renderer.FullName -Raw
$marker = "__GLM52_PROXY_BRIDGE__"
$versionMarker = "__GLM52_PROXY_BRIDGE_RELOAD_V6__"
$proxyUrl = $ProxyBaseUrl.TrimEnd("/")
$snippet = @"
;(()=>{if(globalThis.__GLM52_PROXY_BRIDGE__)return;globalThis.__GLM52_PROXY_BRIDGE__=!0;globalThis.__GLM52_PROXY_BRIDGE_RELOAD_V6__=!0;const u="$proxyUrl/api/admin/zcode/bridge";async function a(c,o,m){try{await fetch(u+"/ack?commandId="+encodeURIComponent(c)+"&ok="+(o?"1":"0")+"&message="+encodeURIComponent(m||""),{cache:"no-store",keepalive:!0})}catch{try{await fetch(u+"/ack",{method:"POST",headers:{"content-type":"application/json"},body:JSON.stringify({commandId:c,ok:o,message:m||""})})}catch{}}}async function y(){try{if(typeof dC!="function"||typeof JS!="function"||typeof Go!="function"||typeof Yo>"u"||typeof Yo.getState!="function"||typeof Q>"u"||typeof Q.getState!="function")return;let c=Q.getState(),o=Yo.getState(),m=Array.isArray(c&&c.tabs)?c.tabs:[],r=dC({tabs:m,baseZCodeSessionService:n.zcodeSessionService,...o});if(!Array.isArray(r)||r.length===0)return;await JS({targets:r,modelProviderService:n.modelProviderService,zcodeSessionService:n.zcodeSessionService,zcodeSessionStore:c});await Promise.allSettled(r.map(async j=>{try{await Go({workspacePath:j.workspacePath,workspaceIdentity:j.workspaceIdentity,remoteSessionId:j.remoteSessionId,localModelProviderService:n.modelProviderService,remoteZCodeSessionService:j.zcodeSessionService||n.zcodeSessionService,force:!0})}catch{}}))}catch{}}function l(c){if(c&&c.reloadRenderer===!1)return;setTimeout(()=>{try{location.reload()}catch{}},1200)}async function p(){let c=null;try{let r=await fetch(u+"/next",{cache:"no-store"});if(!r.ok)return;let j=await r.json();c=j&&j.data&&j.data.command;if(!c||!c.commandId)return;if(c.action==="refreshCodingPlanApiKey"){await a(c.commandId,!0,"ZCode bridge recebeu o comando e iniciou refreshCodingPlanApiKey");let ids=c.providerIds||["builtin:zai-start-plan","builtin:zai-coding-plan"];await Promise.all(ids.map(id=>n.modelProviderService.refreshCodingPlanApiKey(id)));await n.modelProviderService.getAll();await y();l(c)}}catch(e){if(c&&c.commandId)await a(c.commandId,!1,e&&e.message?e.message:String(e))}}setInterval(p,1500);setTimeout(p,500)})();
"@
if ($source.Contains($marker)) {
  $oldSnippetStart = $source.IndexOf(";(()=>{if(globalThis.__GLM52_PROXY_BRIDGE__)return;")
  $oldSnippetEnd = -1
  if ($oldSnippetStart -ge 0) {
    $oldSnippetEnd = $source.IndexOf("setTimeout(p,500)})();", $oldSnippetStart)
  }
  if ($oldSnippetStart -lt 0 -or $oldSnippetEnd -lt 0) {
    throw "Patch encontrado, mas nao foi possivel localizar o bloco para atualizar."
  }
  $oldSnippetEnd += "setTimeout(p,500)})();".Length
  $patched = $source.Substring(0, $oldSnippetStart) + $snippet + $source.Substring($oldSnippetEnd)
  Set-Content -LiteralPath $renderer.FullName -Value $patched -NoNewline
  if ($source.Contains($versionMarker)) {
    Write-Host "Patch v6 atualizado em $($renderer.FullName)."
  } else {
    Write-Host "Patch antigo atualizado para v6 em $($renderer.FullName)."
  }
} else {
  $anchor = 'let n=wUt(t);$9=n,moe(n);'
  if (-not $source.Contains($anchor)) {
    throw "Anchor do ServicePort nao encontrado. Versao do ZCode pode ter mudado."
  }
  $patched = $source.Replace($anchor, $anchor + $snippet)
  Set-Content -LiteralPath $renderer.FullName -Value $patched -NoNewline
  Write-Host "Patch v6 inserido em $($renderer.FullName)."
}

npx --yes @electron/asar pack "$workDir" "$ZCodeAsarPath"

Write-Host "Patch aplicado."
Write-Host "Backup: $backupPath"
Write-Host "Reabra o ZCode para carregar o renderer patchado. Depois disso, a ponte recarrega so a janela ao trocar conta."

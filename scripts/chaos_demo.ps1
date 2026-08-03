# OmniCell AIOps & MCP Engine -- Script del Demo Completo
#
# Uso: .\scripts\chaos_demo.ps1
#      .\scripts\chaos_demo.ps1 -AutoRebalance
#
# Asegurate de que el Gateway este corriendo antes de ejecutar:
#   go run ./cmd/gateway/

param(
    [string]$GatewayUrl = "http://localhost:8080",
    [switch]$AutoRebalance = $false
)

$ErrorActionPreference = "Stop"

# ---- Helpers ----

function Write-Step {
    param([string]$Numero, [string]$Mensaje, [string]$Color = "Cyan")
    Write-Host ""
    Write-Host "[$Numero] $Mensaje" -ForegroundColor $Color
    Write-Host ("-" * 60) -ForegroundColor DarkGray
}

function Get-EstadoTexto {
    param([string]$Estado)
    switch ($Estado) {
        "healthy"   { return "[OK]  " }
        "degraded"  { return "[WARN] " }
        "collapsed" { return "[CRIT] " }
        default     { return "[????] " }
    }
}

function Get-EstadoColor {
    param([string]$Estado)
    switch ($Estado) {
        "healthy"   { return "Green"  }
        "degraded"  { return "Yellow" }
        "collapsed" { return "Red"    }
        default     { return "Gray"   }
    }
}

function Invoke-Gateway {
    param([string]$Method, [string]$Path, [hashtable]$Body = $null)

    $Uri = "$GatewayUrl$Path"
    $Params = @{
        Uri     = $Uri
        Method  = $Method
        Headers = @{ "Content-Type" = "application/json" }
    }

    if ($Body) {
        $Params.Body = $Body | ConvertTo-Json -Depth 10
    }

    try {
        return Invoke-RestMethod @Params
    }
    catch {
        Write-Host "  ERROR: $_" -ForegroundColor Red
        throw
    }
}

function Show-CellStatus {
    param($Celulas)
    foreach ($celula in $Celulas) {
        $texto = Get-EstadoTexto -Estado $celula.estado
        $color = Get-EstadoColor -Estado $celula.estado
        $p99   = $celula.latencia_p99_ms.ToString("F2")
        $reqs  = $celula.total_peticiones
        Write-Host ("  Celula {0}: {1}{2,-10} | p99={3,7} ms | reqs={4}" -f `
            $celula.id, $texto, $celula.estado, $p99, $reqs) -ForegroundColor $color
    }
}

# =============================================================================
# PASO 0: Verificar Gateway
# =============================================================================

Write-Step "0/6" "Verificando disponibilidad del Gateway..." "White"

try {
    $health = Invoke-Gateway -Method "GET" -Path "/api/metrics"
    Write-Host "  OK  Gateway disponible en $GatewayUrl" -ForegroundColor Green
    Write-Host "  OK  Celulas activas: $($health.celulas.Count)" -ForegroundColor Green
}
catch {
    Write-Host "  FAIL  Gateway no disponible." -ForegroundColor Red
    Write-Host "        Ejecuta primero: go run ./cmd/gateway/" -ForegroundColor Yellow
    exit 1
}

Start-Sleep -Seconds 1

# =============================================================================
# PASO 1: Trafico normal (baseline)
# =============================================================================

Write-Step "1/6" "Inyectando trafico NORMAL (baseline)..." "Green"
Write-Host "  -> 3.000 requests distribuidos entre todas las celulas" -ForegroundColor Gray

$loadResult = Invoke-Gateway -Method "POST" -Path "/api/load-test" -Body @{
    cantidad_requests = 3000
    celula_objetivo   = ""
    paralelo          = $false
}

Write-Host "  OK  $($loadResult.mensaje)" -ForegroundColor Green
Start-Sleep -Seconds 2

$metrics = Invoke-Gateway -Method "GET" -Path "/api/metrics"
Write-Host ""
Write-Host "  Estado inicial:" -ForegroundColor Gray
Show-CellStatus -Celulas $metrics.celulas

Start-Sleep -Seconds 2

# =============================================================================
# PASO 2: Pico masivo en Celula C
# =============================================================================

Write-Step "2/6" "Inyectando PICO MASIVO en Celula C..." "Yellow"
Write-Host "  -> 15.000 requests en paralelo concentrados en Celula C" -ForegroundColor Gray
Write-Host "  -> Simulando un evento de flash sale o error en el cliente" -ForegroundColor Gray

$loadResult = Invoke-Gateway -Method "POST" -Path "/api/load-test" -Body @{
    cantidad_requests = 15000
    celula_objetivo   = "C"
    paralelo          = $true
}

Write-Host "  OK  $($loadResult.mensaje)" -ForegroundColor Yellow
Start-Sleep -Seconds 1

# =============================================================================
# PASO 3: Colapso de Celula C
# =============================================================================

Write-Step "3/6" "COLAPSANDO Celula C..." "Red"
Write-Host "  -> La Celula C alcanzo el limite de CPU/I/O" -ForegroundColor Gray
Write-Host "  -> El compartimento hermetico se activa automaticamente" -ForegroundColor Gray

$collapseResult = Invoke-Gateway -Method "POST" -Path "/api/cells/C/collapse"
Write-Host "  BOOM  $($collapseResult.mensaje)" -ForegroundColor Red

Start-Sleep -Seconds 1

$metrics = Invoke-Gateway -Method "GET" -Path "/api/metrics"
Write-Host ""
Write-Host "  Estado post-colapso:" -ForegroundColor Gray
Show-CellStatus -Celulas $metrics.celulas

# =============================================================================
# PASO 4: Pausa para el agente de IA (o auto-simulacion)
# =============================================================================

if (-not $AutoRebalance) {
    Write-Step "4/6" "PAUSA -- El sistema esta en estado critico" "Magenta"
    Write-Host ""
    Write-Host "  Abri tu IDE (Cursor/OpenCode) y escribile al agente de IA:" -ForegroundColor White
    Write-Host ""
    Write-Host '  "Los usuarios reportan lentitud. Revisa el estado del enrutamiento celular."' -ForegroundColor Cyan
    Write-Host ""
    Write-Host "  El agente deberia:" -ForegroundColor Gray
    Write-Host "    1. Ejecutar query_cell_latencies  -> detecta que C esta colapsada" -ForegroundColor Gray
    Write-Host "    2. Proponer rebalance_id_ranges" -ForegroundColor Gray
    Write-Host "    3. Ejecutar el rebalanceo con tu autorizacion" -ForegroundColor Gray
    Write-Host ""
    Write-Host "  Presiona ENTER cuando el agente haya terminado el diagnostico..." -ForegroundColor Yellow
    Read-Host
}
else {
    Write-Step "4/6" "Auto-rebalance activado. Simulando diagnostico del agente de IA..." "Magenta"
    Start-Sleep -Seconds 1
    Write-Host "  [Agente IA] Ejecutando: query_cell_latencies..." -ForegroundColor Cyan
    Start-Sleep -Seconds 2
    Write-Host "  [Agente IA] ALERTA: Celula C COLAPSADA. p99=0ms (sin respuesta)" -ForegroundColor Red
    Start-Sleep -Seconds 1
    Write-Host "  [Agente IA] Solucion: Ejecutar rebalance_id_ranges -> redirigir C hacia A y B" -ForegroundColor Cyan
    Start-Sleep -Seconds 1
    Write-Host "  [Agente IA] Solicitando autorizacion del operador..." -ForegroundColor Cyan
    Start-Sleep -Seconds 1
    Write-Host "  [Operador]  Autorizado" -ForegroundColor Green
    Start-Sleep -Seconds 1
}

# =============================================================================
# PASO 5: Rebalanceo
# =============================================================================

Write-Step "5/6" "Ejecutando REBALANCEO de rangos de IDs..." "Cyan"
Write-Host "  -> Rango de C se redistribuye: mitad a A, mitad a B" -ForegroundColor Gray
Write-Host "  -> Zero-downtime: el sistema no se detiene durante el rebalanceo" -ForegroundColor Gray

$rebalanceBody = @{
    nuevos_rangos = @{
        A = @{ min = 0;       max = 1499999 }
        B = @{ min = 1500000; max = 2999999 }
        C = @{ min = 3000000; max = 3000000 }   # Rango vacio: C no recibe trafico nuevo
    }
}

$rebalanceResult = Invoke-Gateway -Method "POST" -Path "/api/rebalance" -Body $rebalanceBody
Write-Host "  OK  $($rebalanceResult.mensaje)" -ForegroundColor Cyan

Start-Sleep -Seconds 1

# =============================================================================
# PASO 6: Verificacion final
# =============================================================================

Write-Step "6/6" "Verificando estabilizacion del sistema..." "Green"
Start-Sleep -Seconds 2

# Inyectamos mas trafico para demostrar que el sistema sigue operativo
Invoke-Gateway -Method "POST" -Path "/api/load-test" -Body @{
    cantidad_requests = 5000
    celula_objetivo   = ""
    paralelo          = $true
} | Out-Null

Start-Sleep -Seconds 1

$finalMetrics = Invoke-Gateway -Method "GET" -Path "/api/metrics"

Write-Host ""
Write-Host "  Estado final del sistema:" -ForegroundColor Gray
Show-CellStatus -Celulas $finalMetrics.celulas

Write-Host ""
Write-Host ("  Requests/seg:     {0}" -f $finalMetrics.requests_por_segundo.ToString("F1")) -ForegroundColor Green
Write-Host ("  GC ultima pausa:  {0} ms" -f $finalMetrics.gc.ultima_pausa_ms.ToString("F3")) -ForegroundColor Green
Write-Host ("  Cache hit rate:   {0}%" -f $finalMetrics.cache.tasa_hit_porcentaje.ToString("F1")) -ForegroundColor Green

Write-Host ""
Write-Host ("=" * 60) -ForegroundColor DarkGray
Write-Host "  DEMO COMPLETADO EXITOSAMENTE" -ForegroundColor Green
Write-Host "  La Celula C collapso, el sistema detecto el fallo," -ForegroundColor Gray
Write-Host "  rebalanceo el trafico y continuo operando sin downtime." -ForegroundColor Gray
Write-Host ("=" * 60) -ForegroundColor DarkGray
Write-Host ""

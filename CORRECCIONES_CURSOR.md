# Correcciones de Cursor

**Fecha:** 2026-08-03  
**Hora:** 17:28:52 (UTC-3)  
**Contexto:** Demo MCP OmniCell — hallazgos al ejecutar el flujo chaos → diagnóstico → rebalanceo.

---

## Resumen

Durante la prueba de integración MCP se detectaron inconsistencias entre el router, las métricas, el load-test y el reporte de salud del agente. Se aplicaron correcciones en Gateway (Go) y MCP Server (TypeScript).

---

## 1. Rangos desactualizados en `/api/metrics` tras rebalanceo

**Problema:** `POST /api/rebalance` actualizaba solo `router.rangos`. `/api/cells` leía el router (correcto), pero `/api/metrics` leía `Celula.RangoMin` / `RangoMax` (valores de arranque). El MCP (`query_cell_latencies`) mostraba rangos viejos aunque el rebalanceo hubiera funcionado.

**Corrección:** En `Router.Rebalancear` se sincronizan ambos: mapa del router y campos de cada célula.

**Archivo:** `internal/router/router.go`

---

## 2. Load-test no incrementaba `total_peticiones` por célula

**Problema:** `POST /api/load-test` solo llamaba a `EnrutarPorID` y al contador global del colector. Nunca ejecutaba `Celula.Obtener` / `Guardar`, por eso tras 3.000 requests las métricas por célula seguían en 0.

**Corrección:** Cada request del load-test ejecuta `Obtener` sobre la célula enrutada. Además, `EnrutarPorID` ahora devuelve la célula aunque esté colapsada (junto al error), para que los fallos se registren en sus contadores. Rangos drenados (`max < min`) se tratan como fallo seguro (sin panic en `rand.Int63n`).

**Archivos:** `internal/api/handlers.go`, `internal/router/router.go`

---

## 3. Salud MCP siempre “CRÍTICO” tras rebalanceo exitoso

**Problema:** Tras drenar el rango de una célula colapsada, el bulkhead seguía en `collapsed` (correcto), pero `get_system_health` reportaba **CRÍTICO** e insistía en rebalancear aunque el tráfico ya estuviera redirigido.

**Corrección:**
- Si hay células `collapsed` con rango activo (`max > min`) → **CRÍTICO**
- Si están colapsadas pero con rango drenado (`max <= min`) → **MITIGADO**
- `query_cell_latencies` ajusta alertas/recomendaciones en el mismo sentido
- El ejemplo de `rebalance_id_ranges` ahora incluye drenar la célula fallida (p.ej. C con `min=max` sentínela)

**Archivo:** `mcp-server/src/index.ts` (rebuild → `mcp-server/dist`)

---

## 4. Contrato Store/Query con IDs string vs number

**Problema:** `POST /api/query` exigía `"id"` como string; un JSON `"id": 2500000` fallaba al deserializar. Store solo aceptaba `id_numerico` y no devolvía ese valor en la respuesta, lo que complicaba verificar el enrutamiento post-rebalanceo.

**Corrección:**
- Tipo `FlexibleID`: acepta string o number en `id`
- Si `id` es numérico y no viene `id_numerico`, se usa para routing
- Store exige `id_numerico > 0` (o lo deriva de `id` numérico), genera UUID de almacenamiento cuando corresponde, y responde con `id`, `id_numerico` y `celula`

**Archivo:** `internal/api/handlers.go`

---

## Archivos tocados

| Archivo | Cambio |
|---------|--------|
| `internal/router/router.go` | Sync de rangos en rebalanceo; célula retornada si colapsada |
| `internal/api/handlers.go` | Load-test real, FlexibleID, store/query |
| `mcp-server/src/index.ts` | Salud MITIGADO + ejemplos/recomendaciones |
| `mcp-server/dist/*` | Rebuild TypeScript |

---

## Cómo aplicar en runtime

1. **Reiniciar el Gateway** para cargar el binario Go nuevo:
   ```powershell
   go run ./cmd/gateway/
   ```
2. **Recargar el MCP** en Cursor (Settings → MCP → refresh, o Reload Window) para tomar `mcp-server/dist` actualizado.

---

## Verificación sugerida

1. Baseline + load-test → `total_peticiones` > 0 por célula en `/api/metrics`
2. Colapsar C → health **CRÍTICO**
3. Rebalancear drenando C → `/api/metrics` y `/api/cells` con mismos rangos; health **MITIGADO**
4. `POST /api/store` con `id_numerico` en rango ex-C → respuesta con `celula` A o B
5. `POST /api/query` con `"id": 123` (number) → no error de unmarshal

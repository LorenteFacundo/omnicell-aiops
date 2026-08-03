/**
 * index.ts — Servidor MCP de OmniCell
 *
 * Este es el núcleo inteligente del producto: un servidor que implementa
 * el Model Context Protocol (MCP), permitiendo que agentes de IA como
 * Cursor o Claude Desktop interactúen directamente con la infraestructura.
 *
 * ¿Cómo funciona MCP?
 * Un agente de IA (el "cliente MCP") se conecta a este servidor.
 * Cuando el agente necesita información (ej: "¿cómo está el sistema?"),
 * llama a una de las "tools" registradas aquí. El servidor ejecuta la
 * lógica correspondiente y devuelve el resultado al agente.
 *
 * Tools registradas:
 *   - query_cell_latencies: Consulta el estado y latencias del sistema
 *   - rebalance_id_ranges: Redistribuye el tráfico entre células
 *   - simulate_bulkhead_collapse: Colapsa una célula (Chaos Engineering)
 *   - get_system_health: Resumen ejecutivo del estado del sistema
 *
 * Para conectar este servidor a Cursor, agregar al archivo de configuración MCP:
 * Ver mcp-config.json en la raíz del proyecto.
 */

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";

import { gatewayClient, EstadoCelula } from "./gateway-client.js";

// ---- Creación del servidor MCP ----
// McpServer es la clase principal del SDK oficial de Anthropic para servidores MCP.
// Le damos un nombre y versión que el agente de IA verá al conectarse.
const servidor = new McpServer({
  name: "OmniCell AIOps Engine",
  version: "1.0.0",
});

// =============================================================================
// TOOL 1: query_cell_latencies
// Permite al agente de IA leer el estado y latencias de todas las células.
// Esta es la primera tool que la IA ejecuta cuando detecta un problema.
// =============================================================================
servidor.registerTool(
  "query_cell_latencies",
  {
    description:
      "Consulta las latencias, estado de salud y métricas de rendimiento de todas las células del sistema OmniCell. " +
      "Retorna datos de latencia p99, estado (healthy/degraded/collapsed), alertas activas, " +
      "métricas del Garbage Collector de Go, y estadísticas del caché off-heap. " +
      "Usar cuando el usuario reporta lentitud o cuando se quiere verificar el estado del sistema.",
    // Zod schema: define qué parámetros acepta esta tool.
    // query_cell_latencies no necesita parámetros (lee todo el sistema).
    inputSchema: z.object({
      // Solo_celula es opcional: si se especifica, filtra solo esa célula.
      solo_celula: z
        .string()
        .optional()
        .describe(
          "ID de una célula específica para filtrar (ej: 'A', 'B', 'C'). Omitir para ver todas."
        ),
    }),
  },
  // Handler de la tool: la lógica que se ejecuta cuando la IA la invoca
  async ({ solo_celula }) => {
    try {
      // Llamamos al Gateway Go para obtener el snapshot de métricas
      const snapshot = await gatewayClient.obtenerMetricas();

      // Filtramos por célula si se especificó
      let celulas = snapshot.celulas;
      if (solo_celula) {
        celulas = celulas.filter((c) => c.id === solo_celula.toUpperCase());
        if (celulas.length === 0) {
          return {
            content: [
              {
                type: "text",
                text: `Célula '${solo_celula}' no encontrada. Células disponibles: ${snapshot.celulas.map((c) => c.id).join(", ")}`,
              },
            ],
          };
        }
      }

      // Construimos el informe que el agente de IA va a leer
      const lineas: string[] = [];

      lineas.push("## 📊 Estado del Sistema OmniCell");
      lineas.push(
        `**Timestamp:** ${new Date(snapshot.timestamp).toLocaleString()}`
      );
      lineas.push(
        `**Throughput:** ${snapshot.requests_por_segundo.toFixed(1)} req/s`
      );
      lineas.push(
        `**Requests totales:** ${snapshot.total_requests.toLocaleString()}`
      );
      lineas.push(
        `**Tasa de error:** ${((snapshot.requests_fallidos / Math.max(snapshot.total_requests, 1)) * 100).toFixed(2)}%`
      );

      if (snapshot.hay_celulas_colapsadas) {
        const algunaActiva = celulas.some(
          (c) => c.estado === "collapsed" && c.rango_max > c.rango_min
        );
        if (algunaActiva) {
          lineas.push(
            "\n⚠️ **ALERTA CRÍTICA: HAY CÉLULAS COLAPSADAS CON TRÁFICO ACTIVO**"
          );
        } else {
          lineas.push(
            "\n🟠 **ALERTA MITIGADA: hay células colapsadas pero sin rango de tráfico (rebalanceo aplicado)**"
          );
        }
      }

      lineas.push("\n### 🏗️ Estado por Célula");
      for (const celula of celulas) {
        const icono =
          celula.estado === "healthy"
            ? "🟢"
            : celula.estado === "degraded"
              ? "🟡"
              : "🔴";

        lineas.push(`\n**Célula ${celula.id}** ${icono} \`${celula.estado}\``);
        lineas.push(`- Rango de IDs: ${celula.rango_min} → ${celula.rango_max}`);
        lineas.push(`- Latencia p99: **${celula.latencia_p99_ms.toFixed(3)} ms**`);
        lineas.push(
          `- Latencia promedio: ${celula.latencia_promedio_ms.toFixed(3)} ms`
        );
        lineas.push(
          `- Peticiones: ${celula.total_peticiones} (${celula.peticiones_fallidas} fallidas)`
        );
        lineas.push(`- Registros almacenados: ${celula.cantidad_registros}`);

        if (celula.alerta) {
          lineas.push(`- ⚠️ **${celula.mensaje_alerta}**`);
        }
      }

      lineas.push("\n### 🧹 Garbage Collector de Go");
      lineas.push(`- Ciclos de GC: ${snapshot.gc.num_ciclos}`);
      lineas.push(
        `- Última pausa: **${snapshot.gc.ultima_pausa_ms.toFixed(3)} ms** (con BigCache off-heap, debe ser < 1ms)`
      );
      lineas.push(
        `- Pausa total acumulada: ${snapshot.gc.pausa_total_ms.toFixed(1)} ms`
      );
      lineas.push(
        `- Heap en uso: ${snapshot.gc.mem_heap_mb.toFixed(1)} MB`
      );

      lineas.push("\n### ⚡ Caché Off-Heap (BigCache)");
      lineas.push(
        `- Tasa de hit: **${snapshot.cache.tasa_hit_porcentaje.toFixed(1)}%**`
      );
      lineas.push(`- Entradas: ${snapshot.cache.cantidad_entradas}`);
      lineas.push(
        `- Memoria usada: ${snapshot.cache.capacidad_usada_mb.toFixed(1)} MB`
      );

      // Si hay alertas, agregamos recomendaciones de acción
      const celulasConAlerta = celulas.filter((c) => c.alerta);
      const rangoDrenado = (c: { rango_min: number; rango_max: number }) =>
        c.rango_max <= c.rango_min;

      if (celulasConAlerta.length > 0) {
        lineas.push("\n### 🚨 Recomendaciones");
        for (const celula of celulasConAlerta) {
          if (celula.estado === "collapsed" && rangoDrenado(celula)) {
            lineas.push(
              `- Célula **${celula.id}** está COLAPSADA pero su rango está drenado (tráfico ya redirigido). Estado mitigado. Opcional: recover cuando esté lista.`
            );
          } else if (celula.estado === "collapsed") {
            lineas.push(
              `- Célula **${celula.id}** está COLAPSADA. Ejecutar \`rebalance_id_ranges\` incluyendo C (u otra) con rango drenado, p.ej. min=max sentínela.`
            );
          } else if (celula.estado === "degraded") {
            lineas.push(
              `- Célula **${celula.id}** está DEGRADADA. Considerar ejecutar \`rebalance_id_ranges\` preventivamente.`
            );
          }
        }
      }

      return {
        content: [{ type: "text", text: lineas.join("\n") }],
      };
    } catch (error) {
      const mensaje =
        error instanceof Error ? error.message : "Error desconocido";
      return {
        content: [
          {
            type: "text",
            text: `❌ Error al conectar con el Gateway OmniCell: ${mensaje}\n\n¿Está corriendo el Gateway en http://localhost:8080?`,
          },
        ],
        isError: true,
      };
    }
  }
);

// =============================================================================
// TOOL 2: rebalance_id_ranges
// Permite al agente de IA redistribuir el tráfico de una célula colapsada
// a las células sanas. Opera sin detener el sistema (zero-downtime).
// =============================================================================
servidor.registerTool(
  "rebalance_id_ranges",
  {
    description:
      "Redistribuye los rangos de IDs entre células para redirigir el tráfico de una célula colapsada o degradada. " +
      "Opera en caliente (zero-downtime): el sistema no se detiene durante el rebalanceo. " +
      "Usar después de detectar que una célula está colapsada via query_cell_latencies. " +
      "Los rangos del sistema son: Célula A (0-999999), B (1000000-1999999), C (2000000-2999999).",
    inputSchema: z.object({
      // El agente especifica el nuevo rango para cada célula que quiere modificar.
      // Ejemplo: si Célula C colapsó, expandir A y B y drenar C (rango vacío/sentínela).
      nuevos_rangos: z
        .record(
          z.string(),
          z.object({
            min: z.number().describe("ID numérico mínimo del nuevo rango"),
            max: z.number().describe("ID numérico máximo del nuevo rango"),
          })
        )
        .describe(
          'Mapa de célula → nuevo rango. Incluir TODAS las células. Ejemplo al colapsar C: {"A":{"min":0,"max":1499999},"B":{"min":1500000,"max":2999999},"C":{"min":3000000,"max":3000000}}'
        ),

      razon: z
        .string()
        .optional()
        .describe("Razón del rebalanceo (para logging)"),
    }),
  },
  async ({ nuevos_rangos, razon }) => {
    try {
      console.error(
        `[OmniCell MCP] Ejecutando rebalanceo. Razón: ${razon ?? "no especificada"}`
      );

      const resultado = await gatewayClient.rebalancearRangos(nuevos_rangos);

      const lineas: string[] = [];
      lineas.push("## ✅ Rebalanceo Ejecutado Exitosamente");

      if (razon) {
        lineas.push(`**Razón:** ${razon}`);
      }

      lineas.push("\n### 📋 Nuevos Rangos de IDs");
      for (const [celula, rango] of Object.entries(nuevos_rangos)) {
        lineas.push(
          `- **Célula ${celula}:** IDs ${rango.min.toLocaleString()} → ${rango.max.toLocaleString()}`
        );
      }

      lineas.push(
        "\n✅ El tráfico ahora fluye a las células sanas. El sistema opera sin interrupción (zero-downtime)."
      );
      lineas.push(
        "\n💡 Verificar con `get_system_health` (debe pasar a MITIGADO si la célula colapsada quedó con rango drenado) o `query_cell_latencies`."
      );
      lineas.push(
        "\nℹ️ La célula colapsada permanece en estado `collapsed` hasta `simulate_bulkhead_collapse` tipo `recover`."
      );

      return {
        content: [{ type: "text", text: lineas.join("\n") }],
      };
    } catch (error) {
      const mensaje =
        error instanceof Error ? error.message : "Error desconocido";
      return {
        content: [
          {
            type: "text",
            text: `❌ Error al rebalancear: ${mensaje}`,
          },
        ],
        isError: true,
      };
    }
  }
);

// =============================================================================
// TOOL 3: simulate_bulkhead_collapse
// Colapsa una célula intencionalmente para demostrar la resiliencia del sistema.
// Esta es la herramienta de Chaos Engineering del Demo.
// =============================================================================
servidor.registerTool(
  "simulate_bulkhead_collapse",
  {
    description:
      "Simula el colapso de una célula de base de datos para demostrar la resiliencia del sistema. " +
      "Cuando una célula colapsa, las demás continúan operando normalmente (compartimento hermético). " +
      "También puede usarse para degradar una célula (alta latencia sin colapso total). " +
      "Usar con precaución: después del colapso, usar rebalance_id_ranges para redistribuir el tráfico.",
    inputSchema: z.object({
      celula_id: z
        .enum(["A", "B", "C"])
        .describe("ID de la célula a colapsar ('A', 'B', o 'C')"),

      tipo: z
        .enum(["collapse", "degrade", "recover"])
        .describe(
          "'collapse': fallo catastrófico total | 'degrade': alta latencia | 'recover': restaurar a healthy"
        ),
    }),
  },
  async ({ celula_id, tipo }) => {
    try {
      let resultado: { mensaje: string };
      let icono: string;
      let descripcion: string;

      switch (tipo) {
        case "collapse":
          resultado = await gatewayClient.colapsarCelula(celula_id);
          icono = "💥";
          descripcion = "COLAPSADA — no responde a peticiones";
          break;
        case "degrade":
          resultado = await gatewayClient.degradarCelula(celula_id);
          icono = "⚠️";
          descripcion = "DEGRADADA — responde con alta latencia";
          break;
        case "recover":
          resultado = await gatewayClient.recuperarCelula(celula_id);
          icono = "✅";
          descripcion = "RECUPERADA — volvió a estado healthy";
          break;
      }

      const lineas: string[] = [];
      lineas.push(`## ${icono} Célula ${celula_id}: ${descripcion}`);
      lineas.push(`\n**Resultado:** ${resultado.mensaje}`);

      if (tipo === "collapse") {
        lineas.push("\n### 🚨 Sistema en modo de emergencia");
        lineas.push(
          "Los compartimentos herméticos están activados. Las células A y B continúan operando."
        );
        lineas.push("\n**Próximos pasos recomendados:**");
        lineas.push(
          "1. Ejecutar `query_cell_latencies` para confirmar el impacto"
        );
        lineas.push(
          "2. Ejecutar `rebalance_id_ranges` para redistribuir el tráfico de la Célula C"
        );
      } else if (tipo === "recover") {
        lineas.push(
          "\n💡 Recordar ejecutar `rebalance_id_ranges` para restaurar los rangos originales si fueron modificados."
        );
      }

      return {
        content: [{ type: "text", text: lineas.join("\n") }],
      };
    } catch (error) {
      const mensaje =
        error instanceof Error ? error.message : "Error desconocido";
      return {
        content: [
          {
            type: "text",
            text: `❌ Error al modificar la célula ${celula_id}: ${mensaje}`,
          },
        ],
        isError: true,
      };
    }
  }
);

// =============================================================================
// TOOL 4: get_system_health
// Resumen ejecutivo rápido del estado del sistema.
// La IA puede usar esto como "chequeo inicial" antes de profundizar.
// =============================================================================
servidor.registerTool(
  "get_system_health",
  {
    description:
      "Obtiene un resumen ejecutivo del estado de salud del sistema OmniCell. " +
      "Más rápido que query_cell_latencies para una visión general. " +
      "Retorna: estado general (OK/DEGRADADO/MITIGADO/CRÍTICO), células con problemas, " +
      "throughput actual, y tasa de error.",
    inputSchema: z.object({}),
  },
  async () => {
    try {
      const snapshot = await gatewayClient.obtenerMetricas();

      // Determinamos el estado general del sistema.
      // Una célula colapsada con rango drenado (max <= min) ya no recibe tráfico:
      // el fallo está mitigado aunque el bulkhead siga cerrado.
      const rangoDrenado = (c: { rango_min: number; rango_max: number }) =>
        c.rango_max <= c.rango_min;

      const celulasColapsadas = snapshot.celulas.filter(
        (c) => c.estado === "collapsed"
      );
      const colapsadasActivas = celulasColapsadas.filter(
        (c) => !rangoDrenado(c)
      );
      const colapsadasMitigadas = celulasColapsadas.filter((c) =>
        rangoDrenado(c)
      );
      const celulasDegradadas = snapshot.celulas.filter(
        (c) => c.estado === "degraded"
      );

      let estadoGeneral: string;
      let iconoEstado: string;

      if (colapsadasActivas.length > 0) {
        estadoGeneral = "CRÍTICO";
        iconoEstado = "🔴";
      } else if (colapsadasMitigadas.length > 0) {
        estadoGeneral = "MITIGADO";
        iconoEstado = "🟠";
      } else if (celulasDegradadas.length > 0) {
        estadoGeneral = "DEGRADADO";
        iconoEstado = "🟡";
      } else {
        estadoGeneral = "OPERACIONAL";
        iconoEstado = "🟢";
      }

      const tasaError =
        snapshot.total_requests > 0
          ? (
              (snapshot.requests_fallidos / snapshot.total_requests) *
              100
            ).toFixed(2)
          : "0.00";

      const lineas: string[] = [];
      lineas.push(
        `## ${iconoEstado} Sistema OmniCell — Estado: **${estadoGeneral}**`
      );
      lineas.push(
        `**Throughput:** ${snapshot.requests_por_segundo.toFixed(1)} req/s | **Tasa de error:** ${tasaError}%`
      );

      // Resumen de células
      lineas.push("\n| Célula | Estado | Latencia p99 | Alerta |");
      lineas.push("|--------|--------|-------------|--------|");
      for (const celula of snapshot.celulas) {
        const estado =
          celula.estado === "healthy"
            ? "🟢 healthy"
            : celula.estado === "degraded"
              ? "🟡 degraded"
              : "🔴 collapsed";
        const alerta = celula.alerta ? "⚠️ Sí" : "✅ No";
        lineas.push(
          `| ${celula.id} | ${estado} | ${celula.latencia_p99_ms.toFixed(2)} ms | ${alerta} |`
        );
      }

      if (colapsadasActivas.length > 0) {
        lineas.push(
          `\n⚠️ **Células colapsadas con tráfico activo: ${colapsadasActivas.map((c) => c.id).join(", ")}**`
        );
        lineas.push(
          "Ejecutar `rebalance_id_ranges` (incluir la célula colapsada con rango drenado) para redistribuir el tráfico."
        );
      } else if (colapsadasMitigadas.length > 0) {
        lineas.push(
          `\n🟠 **Mitigado:** células colapsadas sin tráfico (rango drenado): ${colapsadasMitigadas.map((c) => c.id).join(", ")}`
        );
        lineas.push(
          "El rebalanceo ya redirigió el tráfico. Opcional: `simulate_bulkhead_collapse` con tipo `recover` cuando la célula esté lista."
        );
      }

      return {
        content: [{ type: "text", text: lineas.join("\n") }],
      };
    } catch (error) {
      const mensaje =
        error instanceof Error ? error.message : "Error desconocido";
      return {
        content: [
          {
            type: "text",
            text: `❌ Gateway no disponible: ${mensaje}`,
          },
        ],
        isError: true,
      };
    }
  }
);

// =============================================================================
// Inicio del servidor MCP
// Usamos StdioServerTransport: el servidor se comunica via stdin/stdout.
// Esto es lo que permite que Cursor o Claude Desktop lo ejecuten como proceso hijo.
// =============================================================================
async function main() {
  const transport = new StdioServerTransport();
  await servidor.connect(transport);

  // Los logs van a stderr para no interferir con el protocolo MCP en stdout
  console.error("✅ OmniCell MCP Server iniciado");
  console.error(`   Conectado a Gateway: ${process.env.GATEWAY_URL ?? "http://localhost:8080"}`);
  console.error("   Tools disponibles:");
  console.error("     - query_cell_latencies");
  console.error("     - rebalance_id_ranges");
  console.error("     - simulate_bulkhead_collapse");
  console.error("     - get_system_health");
}

main().catch((error) => {
  console.error("Error fatal en MCP Server:", error);
  process.exit(1);
});

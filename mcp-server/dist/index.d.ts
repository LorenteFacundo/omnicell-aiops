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
export {};
//# sourceMappingURL=index.d.ts.map
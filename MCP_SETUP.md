# 🔌 Guía de Configuración del Servidor MCP para Cursor / OpenCode

Este documento detalla cómo registrar e integrar el **OmniCell MCP Server** en **Cursor**, **OpenCode** o **Claude Desktop** para habilitar la resolución autónoma de incidentes por parte de la IA.

---

## 🛠️ Requisitos Previos

Antes de configurar el MCP en tu IDE:

1. **Compilar el Servidor MCP**:
   Asegúrate de que la carpeta `mcp-server/dist` exista. Si no existe, compílala ejecutando:
   ```powershell
   cd mcp-server
   npm install
   npm run build
   ```

2. **Gateway en Ejecución**:
   El Gateway Go debe estar escuchando en `http://localhost:8080`:
   ```powershell
   go run ./cmd/gateway/
   ```

---

## 🚀 Configuración en Cursor

### Método A: Desde la Interfaz de Cursor (Recomendado)

1. Abre **Cursor**.
2. Ve a **Settings** (`Ctrl + ,` o `Cmd + ,`).
3. En el menú lateral izquierdo, selecciona **Features** -> **MCP Servers**.
4. Haz clic en **+ Add New MCP Server**.
5. Completa los campos con la siguiente información:

   * **Name**: `omnicell`
   * **Type**: `command` (Stdio)
   * **Command**: `node d:/000_Servi/Proyectos de Servi/OMNICELL/mcp-server/dist/index.js`

6. Guarda los cambios. Verás un indicador verde 🟢 confirmando que el servidor MCP se ha conectado.

---

### Método B: Edición Directa del Archivo `mcp.json`

Si prefieres editar el archivo de configuración de Cursor directamente:

* **Ruta en Windows**: `%APPDATA%\Cursor\User\globalStorage\mcp.json` (o en la configuración global de Cursor).

Agrega la siguiente clave dentro del objeto `"mcpServers"`:

```json
{
  "mcpServers": {
    "omnicell": {
      "command": "node",
      "args": [
        "d:/000_Servi/Proyectos de Servi/OMNICELL/mcp-server/dist/index.js"
      ],
      "env": {
        "GATEWAY_URL": "http://localhost:8080"
      }
    }
  }
}
```

---

## 💻 Configuración en OpenCode / Claude Desktop

Para **OpenCode** o **Claude Desktop**, agrega la configuración al archivo `claude_desktop_config.json` o `opencode.json`:

* **Ruta en Windows (Claude Desktop)**: `%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "omnicell": {
      "command": "node",
      "args": [
        "d:/000_Servi/Proyectos de Servi/OMNICELL/mcp-server/dist/index.js"
      ],
      "env": {
        "GATEWAY_URL": "http://localhost:8080"
      }
    }
  }
}
```

---

## 🧰 Herramientas MCP Expuestas a la IA

Una vez conectado, tu asistente de IA tendrá acceso a las siguientes herramientas:

| Herramienta | Descripción | Parámetros |
|---|---|---|
| `query_cell_latencies` | Consulta la latencia p99, métricas de GC y estado de salud de todas las células | `solo_celula` (opcional: "A", "B", "C") |
| `rebalance_id_ranges` | Reconfigura en caliente (zero-downtime) los rangos de IDs para redirigir tráfico | `nuevos_rangos` (objeto mapa), `razon` |
| `simulate_bulkhead_collapse` | Inyecta fallos de Chaos Engineering (colapso/degradación) o recupera células | `celula_id` ("A", "B", "C"), `tipo` ("collapse", "degrade", "recover") |
| `get_system_health` | Resumen ejecutivo rápido del estado operacional global del sistema | Ninguno |

---

## 🧪 Cómo Probar la Integración (Demo)

1. En una terminal, ejecuta el script de simulación de chaos:
   ```powershell
   .\scripts\chaos_demo.ps1
   ```
2. Cuando la consola se pause en el **Paso 4** (con la Célula C colapsada), abre el panel de chat de Cursor y escribe:
   > *"Los usuarios reportan latencia y errores. Revisa el estado del enrutamiento celular de OmniCell y soluciona cualquier problema."*
3. Verás cómo Cursor invoca automáticamente `query_cell_latencies`, detecta el colapso de la Célula C y solicita tu confirmación para invocar `rebalance_id_ranges`.

// tools.go — Definiciones de herramientas y ejecutor
//
// Este archivo tiene dos responsabilidades:
//  1. GetToolDefinitions(): devuelve los esquemas JSON de las herramientas
//     que el LLM puede invocar (Function Calling).
//  2. ToolExecutor: ejecuta las herramientas cuando el LLM las pide.
//
// El LLM lee los esquemas para saber QUÉ puede hacer.
// Cuando decide usarlos, nos devuelve un ToolCall con nombre y argumentos.
// ToolExecutor.Execute() recibe ese ToolCall y llama al sistema real.
package ai

import (
	"encoding/json"
	"fmt"

	"github.com/omnicell/aiops-engine/internal/metrics"
	"github.com/omnicell/aiops-engine/internal/router"
)

// GetToolDefinitions devuelve las herramientas disponibles para el LLM.
// Cada herramienta tiene un esquema JSON Schema que el LLM lee para saber
// qué parámetros acepta y cuándo usarla.
func GetToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name: "get_system_metrics",
			Description: "Obtiene el estado completo del sistema OmniCell: latencia p99 de cada celula, " +
				"estado de salud (healthy/degraded/collapsed), metricas del Garbage Collector de Go, " +
				"tasa de cache hit, y throughput actual. Llamar PRIMERO cuando el usuario reporta " +
				"problemas o como punto de partida del diagnostico.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []string{},
			},
		},
		{
			Name: "rebalance_cell_ranges",
			Description: "Redistribuye los rangos de IDs entre celulas para redirigir el trafico " +
				"de una celula colapsada o degradada a celulas sanas. Opera en caliente (zero-downtime): " +
				"el sistema no se detiene durante el rebalanceo. " +
				"Rangos normales: A=0-999999, B=1000000-1999999, C=2000000-2999999. " +
				"Para vaciar una celula, asignar min=max=un numero fuera del rango (ej: 3000000).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"nuevos_rangos": map[string]interface{}{
						"type":        "object",
						"description": `Mapa de celula_id -> {min, max}. Ejemplo para absorber C en A y B: {"A": {"min": 0, "max": 1499999}, "B": {"min": 1500000, "max": 2999999}, "C": {"min": 3000000, "max": 3000000}}`,
						"additionalProperties": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"min": map[string]interface{}{"type": "integer", "description": "ID minimo del rango"},
								"max": map[string]interface{}{"type": "integer", "description": "ID maximo del rango"},
							},
							"required": []string{"min", "max"},
						},
					},
					"razon": map[string]interface{}{
						"type":        "string",
						"description": "Razon del rebalanceo para el log del sistema",
					},
				},
				"required": []string{"nuevos_rangos"},
			},
		},
		{
			Name: "set_cell_state",
			Description: "Cambia el estado de una celula especifica. " +
				"'collapse': fallo catastrofico total (sin respuesta). " +
				"'degrade': alta latencia pero sigue respondiendo. " +
				"'recover': restaura la celula a estado healthy.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"celula_id": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"A", "B", "C"},
						"description": "ID de la celula a modificar",
					},
					"estado": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"collapse", "degrade", "recover"},
						"description": "Nuevo estado: collapse (fallo total), degrade (alta latencia), recover (restaurar)",
					},
				},
				"required": []string{"celula_id", "estado"},
			},
		},
	}
}

// ToolExecutor ejecuta las herramientas que el LLM decide invocar.
// Tiene acceso directo al router y al colector de métricas del Gateway,
// lo que significa que opera sin overhead HTTP (todo en memoria).
type ToolExecutor struct {
	router  *router.Router
	colector *metrics.Colector
}

// NewToolExecutor crea el ejecutor inyectando las dependencias del sistema.
func NewToolExecutor(r *router.Router, col *metrics.Colector) *ToolExecutor {
	return &ToolExecutor{router: r, colector: col}
}

// Execute recibe un ToolCall del LLM, ejecuta la herramienta correspondiente,
// y devuelve el resultado como string JSON.
// El string resultante se envía de vuelta al LLM como "tool result".
func (te *ToolExecutor) Execute(tc ToolCall) string {
	switch tc.Name {
	case "get_system_metrics":
		return te.executeGetMetrics()
	case "rebalance_cell_ranges":
		return te.executeRebalance(tc.Arguments)
	case "set_cell_state":
		return te.executeSetCellState(tc.Arguments)
	default:
		return fmt.Sprintf(`{"error": "herramienta desconocida: %s"}`, tc.Name)
	}
}

// executeGetMetrics obtiene el snapshot completo del sistema y lo devuelve como JSON.
// Es la herramienta más importante: le da "ojos" al LLM para ver el sistema.
func (te *ToolExecutor) executeGetMetrics() string {
	snapshot := te.colector.ObtenerSnapshot()
	data, err := json.Marshal(snapshot)
	if err != nil {
		return `{"error": "no se pudieron serializar las metricas"}`
	}
	return string(data)
}

// executeRebalance parsea los argumentos del LLM y ejecuta el rebalanceo de rangos.
func (te *ToolExecutor) executeRebalance(argsJSON string) string {
	// Estructura que esperamos del LLM
	var args struct {
		NuevosRangos map[string]struct {
			Min int64 `json:"min"`
			Max int64 `json:"max"`
		} `json:"nuevos_rangos"`
		Razon string `json:"razon"`
	}

	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf(`{"error": "argumentos invalidos: %s"}`, err.Error())
	}

	if len(args.NuevosRangos) == 0 {
		return `{"error": "nuevos_rangos no puede estar vacio"}`
	}

	// Convertimos al tipo que usa el router
	rangos := make(map[string]router.RangoID)
	for id, r := range args.NuevosRangos {
		rangos[id] = router.RangoID{Min: r.Min, Max: r.Max}
	}

	if err := te.router.Rebalancear(rangos); err != nil {
		return fmt.Sprintf(`{"error": "rebalanceo fallido: %s"}`, err.Error())
	}

	razon := args.Razon
	if razon == "" {
		razon = "rebalanceo ejecutado por el agente IA"
	}

	return fmt.Sprintf(
		`{"exito": true, "mensaje": "Rangos rebalanceados exitosamente. Razon: %s", "celulas_afectadas": %d}`,
		razon, len(args.NuevosRangos),
	)
}

// executeSetCellState parsea los argumentos del LLM y cambia el estado de una célula.
func (te *ToolExecutor) executeSetCellState(argsJSON string) string {
	var args struct {
		CelulaID string `json:"celula_id"`
		Estado   string `json:"estado"`
	}

	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf(`{"error": "argumentos invalidos: %s"}`, err.Error())
	}

	// Obtenemos la célula del router
	celula, existe := te.router.ObtenerCelula(args.CelulaID)
	if !existe {
		return fmt.Sprintf(`{"error": "celula '%s' no encontrada. Validas: A, B, C"}`, args.CelulaID)
	}

	// Aplicamos el cambio de estado
	switch args.Estado {
	case "collapse":
		celula.SimularColapso()
		return fmt.Sprintf(`{"exito": true, "mensaje": "Celula %s colapsada exitosamente"}`, args.CelulaID)
	case "degrade":
		celula.SimularDegradacion()
		return fmt.Sprintf(`{"exito": true, "mensaje": "Celula %s degradada exitosamente"}`, args.CelulaID)
	case "recover":
		celula.Recuperar()
		return fmt.Sprintf(`{"exito": true, "mensaje": "Celula %s recuperada exitosamente"}`, args.CelulaID)
	default:
		return fmt.Sprintf(`{"error": "estado invalido: '%s'. Validos: collapse, degrade, recover"}`, args.Estado)
	}
}

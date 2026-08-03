// Package ai implementa el motor de Inteligencia Artificial nativo de OmniCell.
//
// Este paquete permite que el sistema se comunique directamente con LLMs
// (OpenAI, Anthropic Claude, Google Gemini, Groq, Ollama) sin necesidad de
// ningún IDE o herramienta externa.
//
// El motor opera en dos modos:
//  1. Chat Manual: El operador escribe desde el Dashboard y la IA responde.
//  2. Guardián Auto-Healing: Un goroutine monitorea el sistema. Si detecta
//     un colapso, activa la IA automáticamente para diagnosticar y reparar.
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/omnicell/aiops-engine/internal/metrics"
	"github.com/omnicell/aiops-engine/internal/router"
)

// ---- Tipos de mensajes de conversación ----

// Role identifica quién emite un mensaje en la conversación
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool" // resultado de una herramienta ejecutada
)

// Message representa un turno en la conversación con el LLM.
// Es el formato interno — cada proveedor lo convierte a su propio formato.
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // solo en mensajes de asistente
	ToolCallID string     `json:"tool_call_id,omitempty"` // solo en mensajes de tipo tool
	Name       string     `json:"name,omitempty"`         // nombre de la herramienta ejecutada
}

// ToolCall es una invocación de herramienta pedida por el LLM.
// El LLM decide qué herramienta usar y con qué argumentos.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON serializado (para uniformidad entre proveedores)
}

// ToolDefinition define el esquema JSON de una herramienta para el LLM.
// El LLM lee estas definiciones para saber qué herramientas tiene disponibles.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"` // JSON Schema
}

// ChatResponse es la respuesta normalizada del LLM (independiente del proveedor).
type ChatResponse struct {
	Content   string     `json:"content,omitempty"` // texto de respuesta
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ---- Configuración y catálogo de proveedores ----

// ProviderType identifica el proveedor de IA
type ProviderType string

const (
	ProviderOpenAI    ProviderType = "openai"
	ProviderAnthropic ProviderType = "anthropic"
	ProviderGemini    ProviderType = "gemini"
	ProviderGroq      ProviderType = "groq"
	ProviderOllama    ProviderType = "ollama"
)

// ModeloDisponible representa una opción de modelo para mostrar en el UI
type ModeloDisponible struct {
	ID          string `json:"id"`
	Nombre      string `json:"nombre"`
	Descripcion string `json:"descripcion"`
}

// ModelosPorProveedor es el catálogo de modelos soportados por cada proveedor.
// El Dashboard usa esto para poblar el selector de modelos.
var ModelosPorProveedor = map[ProviderType][]ModeloDisponible{
	ProviderOpenAI: {
		{"gpt-4o", "GPT-4o", "El más capaz, ideal para razonamiento complejo"},
		{"gpt-4o-mini", "GPT-4o Mini", "Más rápido y más económico"},
	},
	ProviderAnthropic: {
		{"claude-3-5-sonnet-20241022", "Claude 3.5 Sonnet", "El más capaz de Anthropic"},
		{"claude-3-haiku-20240307", "Claude 3 Haiku", "Ultra-rápido y económico"},
	},
	ProviderGemini: {
		{"gemini-2.0-flash", "Gemini 2.0 Flash", "El más nuevo de Google, muy rápido"},
		{"gemini-1.5-flash", "Gemini 1.5 Flash", "Rápido y eficiente"},
	},
	ProviderGroq: {
		{"llama-3.1-70b-versatile", "Llama 3.1 70B (Groq)", "Ultra-rápido via Groq Cloud, gratuito"},
		{"llama3-8b-8192", "Llama 3 8B (Groq)", "Muy rápido y liviano"},
	},
	ProviderOllama: {
		{"llama3.1", "Llama 3.1 8B (local)", "LLM local, sin costos, sin internet"},
		{"qwen2.5:7b", "Qwen 2.5 7B (local)", "Alternativa local de alta calidad"},
	},
}

// Config contiene la configuración completa del motor de IA.
// Se guarda en memoria (no persiste al reiniciar el Gateway).
type Config struct {
	Proveedor   ProviderType `json:"proveedor"`
	APIKey      string       `json:"api_key,omitempty"` // omitted in GET responses
	Modelo      string       `json:"modelo"`
	BaseURL     string       `json:"base_url,omitempty"` // para Ollama local
	AutoHealing bool         `json:"auto_healing"`
	Configurado bool         `json:"configurado"`
}

// ConfigPublica es la configuración que se devuelve en el endpoint GET /api/ai/config.
// No incluye la API Key por seguridad.
type ConfigPublica struct {
	Proveedor   ProviderType       `json:"proveedor"`
	Modelo      string             `json:"modelo"`
	BaseURL     string             `json:"base_url,omitempty"`
	AutoHealing bool               `json:"auto_healing"`
	Configurado bool               `json:"configurado"`
	TieneAPIKey bool               `json:"tiene_api_key"`
	Modelos     []ModeloDisponible `json:"modelos_disponibles"`
}

// ---- Provider interface ----

// Provider es la interfaz que todos los clientes de LLM deben implementar.
// Permite cambiar de proveedor sin modificar el motor central.
type Provider interface {
	Complete(ctx context.Context, messages []Message, tools []ToolDefinition) (*ChatResponse, error)
	Nombre() string
}

// ---- Historial de conversaciones ----

// HistoryEntry registra una sesión de conversación (chat manual o auto-healing).
type HistoryEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Tipo      string    `json:"tipo"`    // "chat" | "auto_healing" | "auto_healing_error"
	Resumen   string    `json:"resumen"` // respuesta final del LLM (texto)
	ToolCalls int       `json:"tool_calls_ejecutados"`
}

// ---- ChatResult ----

// ChatResult es la respuesta completa de una sesión de chat.
// Se devuelve en el endpoint POST /api/ai/chat.
type ChatResult struct {
	Respuesta string    `json:"respuesta"`
	Mensajes  []Message `json:"mensajes"` // historial completo de la sesión
	ToolCalls int       `json:"tool_calls_ejecutados"`
}

// ---- AIEngine ----

// systemPrompt es el prompt de sistema del agente AIOps.
// Define el rol, el contexto arquitectónico y el protocolo de diagnóstico.
const systemPrompt = `Eres el Agente AIOps Guardian de OmniCell, un motor de infraestructura distribuida con arquitectura celular.

ARQUITECTURA DEL SISTEMA:
- Tres celulas independientes: A (IDs 0-999999), B (IDs 1000000-1999999), C (IDs 2000000-2999999)
- Cada celula puede estar: healthy (operando normal), degraded (alta latencia), collapsed (sin respuesta)
- Cache off-heap (BigCache) evita pausas del GC. Una GC pausa alta indica problemas de memoria.
- Latencia p99 > 50ms indica saturacion. p99 = 0ms en celula collapsed significa sin respuesta.

PROTOCOLO DE DIAGNOSTICO:
1. Llama primero a get_system_metrics para ver el estado actual.
2. Analiza cuales celulas tienen alertas y por que.
3. Si hay celulas colapsadas, usa rebalance_cell_ranges para redirigir su trafico.
4. Explica tu razonamiento antes de ejecutar una accion.
5. Confirma el resultado llamando a get_system_metrics nuevamente si es necesario.

REBALANCEO ESTANDAR (cuando Celula C colapsa):
- Celula A nuevo rango: {min: 0, max: 1499999} (absorbe mitad del rango de C)
- Celula B nuevo rango: {min: 1500000, max: 2999999} (absorbe otra mitad de C)
- Celula C nuevo rango: {min: 3000000, max: 3000000} (rango vacio: no recibe trafico nuevo)

Responde en espanol. Se conciso pero informativo. Si detectas un problema, propone y ejecuta la solucion.`

// AIEngine es el motor principal de inteligencia artificial.
// Maneja la configuración, el historial, el loop del guardián y las conversaciones.
type AIEngine struct {
	mu       sync.RWMutex
	config   Config
	executor *ToolExecutor
	historia []HistoryEntry

	// Para controlar el goroutine del guardian
	stopGuardian chan struct{}
}

// NewAIEngine crea un nuevo motor de IA con las dependencias inyectadas.
func NewAIEngine(r *router.Router, col *metrics.Colector) *AIEngine {
	return &AIEngine{
		executor:     NewToolExecutor(r, col),
		historia:     make([]HistoryEntry, 0, 50),
		stopGuardian: make(chan struct{}, 1), // buffer de 1 para no bloquear StopGuardian
	}
}

// SetConfig actualiza la configuración del motor de IA.
// Thread-safe: puede llamarse desde cualquier goroutine.
func (e *AIEngine) SetConfig(cfg Config) {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	// Si no enviaron una nueva APIKey pero ya había una guardada, la preservamos
	if cfg.APIKey == "" && e.config.APIKey != "" {
		cfg.APIKey = e.config.APIKey
	}
	
	cfg.Configurado = true
	e.config = cfg
}

// GetConfig devuelve la configuración pública (sin API Key).
func (e *AIEngine) GetConfig() ConfigPublica {
	e.mu.RLock()
	defer e.mu.RUnlock()

	modelos := ModelosPorProveedor[e.config.Proveedor]
	return ConfigPublica{
		Proveedor:   e.config.Proveedor,
		Modelo:      e.config.Modelo,
		BaseURL:     e.config.BaseURL,
		AutoHealing: e.config.AutoHealing,
		Configurado: e.config.Configurado,
		TieneAPIKey: e.config.APIKey != "",
		Modelos:     modelos,
	}
}

// SetAutoHealing activa o desactiva el Guardián Autónomo.
func (e *AIEngine) SetAutoHealing(activo bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config.AutoHealing = activo
}

// EstaConfigurado devuelve true si el motor tiene una API Key configurada.
func (e *AIEngine) EstaConfigurado() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	// Para Ollama no se necesita API Key
	if e.config.Proveedor == ProviderOllama {
		return e.config.Configurado && e.config.Modelo != ""
	}
	return e.config.Configurado && e.config.APIKey != ""
}

// GetHistoria devuelve el historial de conversaciones.
func (e *AIEngine) GetHistoria() []HistoryEntry {
	e.mu.RLock()
	defer e.mu.RUnlock()
	copia := make([]HistoryEntry, len(e.historia))
	copy(copia, e.historia)
	return copia
}

// getProvider crea el proveedor correcto según la configuración actual.
// PRECONDICIÓN: el caller debe tener e.mu.RLock() adquirido.
func (e *AIEngine) getProvider() (Provider, error) {
	cfg := e.config
	switch cfg.Proveedor {
	case ProviderOpenAI:
		return NewOpenAICompatibleProvider(cfg.APIKey, cfg.Modelo, "https://api.openai.com/v1/chat/completions"), nil
	case ProviderGroq:
		return NewOpenAICompatibleProvider(cfg.APIKey, cfg.Modelo, "https://api.groq.com/openai/v1/chat/completions"), nil
	case ProviderOllama:
		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		return NewOpenAICompatibleProvider("ollama", cfg.Modelo, baseURL+"/v1/chat/completions"), nil
	case ProviderAnthropic:
		return NewAnthropicProvider(cfg.APIKey, cfg.Modelo), nil
	case ProviderGemini:
		return NewOpenAICompatibleProvider(cfg.APIKey, cfg.Modelo, "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"), nil
	default:
		return nil, fmt.Errorf("proveedor desconocido: %s", cfg.Proveedor)
	}
}

// Chat ejecuta una conversación con el LLM incluyendo el loop de function calling.
//
// El flujo es:
//  1. Enviamos el mensaje + herramientas al LLM.
//  2. Si el LLM pide ejecutar herramientas, las ejecutamos y enviamos los resultados.
//  3. Repetimos hasta que el LLM devuelva texto puro (sin tool calls).
func (e *AIEngine) Chat(ctx context.Context, userMessage string) (*ChatResult, error) {
	return e.runChat(ctx, userMessage, "chat")
}

// autoHeal es como Chat pero registra la sesión como "auto_healing" en el historial.
// Solo lo llama el guardián automático.
func (e *AIEngine) autoHeal(ctx context.Context, prompt string) (*ChatResult, error) {
	return e.runChat(ctx, prompt, "auto_healing")
}

// runChat es la implementación central del agentic loop.
func (e *AIEngine) runChat(ctx context.Context, userMessage string, tipo string) (*ChatResult, error) {
	e.mu.RLock()
	provider, err := e.getProvider()
	e.mu.RUnlock()

	if err != nil {
		return nil, err
	}

	tools := GetToolDefinitions()

	// Iniciamos la conversación con el prompt de sistema y el mensaje del usuario
	messages := []Message{
		{Role: RoleSystem, Content: systemPrompt},
		{Role: RoleUser, Content: userMessage},
	}

	toolCallsEjecutados := 0
	const maxIteraciones = 8 // límite de iteraciones para evitar loops infinitos

	// Agentic loop: LLM → ejecutar tools → LLM → ... → respuesta final
	for i := 0; i < maxIteraciones; i++ {
		response, err := provider.Complete(ctx, messages, tools)
		if err != nil {
			return nil, fmt.Errorf("error consultando %s: %w", provider.Nombre(), err)
		}

		// Agregamos la respuesta del LLM al historial de la conversación
		messages = append(messages, Message{
			Role:      RoleAssistant,
			Content:   response.Content,
			ToolCalls: response.ToolCalls,
		})

		// Si no hay tool calls, el LLM terminó → salimos del loop
		if len(response.ToolCalls) == 0 {
			break
		}

		// Ejecutamos cada tool call y agregamos los resultados
		for _, tc := range response.ToolCalls {
			resultado := e.executor.Execute(tc)
			toolCallsEjecutados++

			messages = append(messages, Message{
				Role:       RoleTool,
				Content:    resultado,
				ToolCallID: tc.ID,
				Name:       tc.Name,
			})
		}
	}

	// Extraemos la respuesta final (último mensaje del asistente con texto)
	respuestaFinal := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == RoleAssistant && messages[i].Content != "" {
			respuestaFinal = messages[i].Content
			break
		}
	}

	// Guardamos la sesión en el historial (limitado a 50 entradas)
	e.mu.Lock()
	e.historia = append(e.historia, HistoryEntry{
		Timestamp: time.Now(),
		Tipo:      tipo,
		Resumen:   respuestaFinal,
		ToolCalls: toolCallsEjecutados,
	})
	if len(e.historia) > 50 {
		e.historia = e.historia[len(e.historia)-50:]
	}
	e.mu.Unlock()

	return &ChatResult{
		Respuesta: respuestaFinal,
		Mensajes:  messages,
		ToolCalls: toolCallsEjecutados,
	}, nil
}

// ---- Guardián Autónomo (Auto-Healing) ----

// StartGuardian inicia el goroutine del Guardián Autónomo en background.
// Debe llamarse una sola vez al iniciar el Gateway.
func (e *AIEngine) StartGuardian() {
	go e.guardianLoop()
}

// StopGuardian detiene el guardián de forma graceful.
func (e *AIEngine) StopGuardian() {
	select {
	case e.stopGuardian <- struct{}{}:
	default:
	}
}

// guardianLoop es el loop principal del Guardián Autónomo.
// Se ejecuta en un goroutine separado.
func (e *AIEngine) guardianLoop() {
	// Revisamos el sistema cada 5 segundos
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Evitamos activar el auto-healing más de una vez cada 60 segundos
	// (para no spamear la API si el sistema está en un estado persistente de crisis)
	var ultimaRemediacion time.Time

	for {
		select {
		case <-e.stopGuardian:
			return

		case <-ticker.C:
			e.mu.RLock()
			autoHealing := e.config.AutoHealing
			e.mu.RUnlock()

			if !autoHealing || !e.EstaConfigurado() {
				continue
			}

			// Leemos las métricas del sistema directamente
			snapshot := e.executor.colector.ObtenerSnapshot()

			// Solo actuamos si hay un colapso Y no intervenimos recientemente
			if !snapshot.HayCelulasColapsadas {
				continue
			}
			if time.Since(ultimaRemediacion) < 60*time.Second {
				continue
			}

			ultimaRemediacion = time.Now()

			// Construimos el prompt de emergencia con el estado actual del sistema
			snapshotJSON, _ := json.Marshal(snapshot)
			prompt := fmt.Sprintf(
				"ALERTA AUTOMATICA DEL GUARDIAN AIOPS:\n\nEl sistema de monitoreo detecto el siguiente estado critico:\n%s\n\n"+
					"Analiza la situacion y toma las acciones necesarias para restaurar el servicio. "+
					"Explica tu diagnostico y las acciones que vas a tomar antes de ejecutarlas.",
				string(snapshotJSON),
			)

			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			_, err := e.autoHeal(ctx, prompt)
			cancel()

			if err != nil {
				// Registramos el error en el historial
				e.mu.Lock()
				e.historia = append(e.historia, HistoryEntry{
					Timestamp: time.Now(),
					Tipo:      "auto_healing_error",
					Resumen:   fmt.Sprintf("Error del Guardian: %v", err),
				})
				e.mu.Unlock()
			}
		}
	}
}

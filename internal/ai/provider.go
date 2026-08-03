// provider.go — Implementación de los clientes HTTP para las APIs de IA.
//
// Este archivo contiene la implementación del cliente compatible con la API de OpenAI.
// La gran ventaja de la API de OpenAI es que se ha convertido en el estándar de facto.
// Proveedores como Groq y Ollama soportan nativamente la misma estructura de requests
// (incluyendo Function Calling), lo que nos permite usar este mismo cliente para los tres.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAICompatibleProvider es un cliente para cualquier API que hable el protocolo de OpenAI
// (OpenAI, Groq, Ollama, etc).
type OpenAICompatibleProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// NewOpenAICompatibleProvider inicializa un nuevo proveedor.
func NewOpenAICompatibleProvider(apiKey, model, baseURL string) *OpenAICompatibleProvider {
	return &OpenAICompatibleProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 60 * time.Second, // Timeout generoso para LLMs
		},
	}
}

func (p *OpenAICompatibleProvider) Nombre() string {
	return "OpenAI/Compatible"
}

// Tipos internos para serializar el request de OpenAI

type openAIChatRequest struct {
	Model       string           `json:"model"`
	Messages    []openAIMessage  `json:"messages"`
	Tools       []openAITool     `json:"tools,omitempty"`
	ToolChoice  string           `json:"tool_choice,omitempty"` // "auto"
	Temperature float32          `json:"temperature"`
}

type openAIMessage struct {
	Role       string              `json:"role"`
	Content    string              `json:"content,omitempty"`
	Name       string              `json:"name,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall    `json:"tool_calls,omitempty"`
}

type openAITool struct {
	Type     string         `json:"type"` // siempre "function"
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Role      string           `json:"role"`
			Content   string           `json:"content"`
			ToolCalls []openAIToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Complete envía la conversación al LLM y parsea la respuesta.
func (p *OpenAICompatibleProvider) Complete(ctx context.Context, messages []Message, tools []ToolDefinition) (*ChatResponse, error) {
	reqBody := openAIChatRequest{
		Model:       p.model,
		Temperature: 0.1, // Temperatura baja para decisiones deterministas en infraestructura
	}

	// 1. Mapear mensajes internos al formato de OpenAI
	for _, m := range messages {
		oaiMsg := openAIMessage{
			Role:       string(m.Role),
			Content:    m.Content,
			Name:       m.Name,
			ToolCallID: m.ToolCallID,
		}

		// Si el mensaje tiene ToolCalls (asistente), los mapeamos
		for _, tc := range m.ToolCalls {
			oaiMsg.ToolCalls = append(oaiMsg.ToolCalls, openAIToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: openAIFunctionCall{
					Name:      tc.Name,
					Arguments: tc.Arguments,
				},
			})
		}
		reqBody.Messages = append(reqBody.Messages, oaiMsg)
	}

	// 2. Mapear herramientas (Function Calling schemas)
	if len(tools) > 0 {
		for _, t := range tools {
			reqBody.Tools = append(reqBody.Tools, openAITool{
				Type: "function",
				Function: openAIFunction{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.Parameters,
				},
			})
		}
		reqBody.ToolChoice = "auto"
	}

	// 3. Serializar y enviar request HTTP
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("error serializando request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" && p.apiKey != "ollama" { // Ollama local no usa API Key
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error de red contactando API IA: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error API (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	// 4. Parsear respuesta
	var apiResp openAIChatResponse
	if err := json.Unmarshal(bodyBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("error parseando respuesta: %w", err)
	}

	if apiResp.Error.Message != "" {
		return nil, fmt.Errorf("API respondio error: %s", apiResp.Error.Message)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("API no devolvio respuestas (choices vacio)")
	}

	// 5. Mapear respuesta de vuelta a nuestro formato interno
	msg := apiResp.Choices[0].Message
	result := &ChatResponse{
		Content: msg.Content,
	}

	for _, tc := range msg.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}

	return result, nil
}

// --------------------------------------------------------------------------------
// NOTA: Para Anthropic (Claude) y Google (Gemini), se implementarian clientes similares
// que mapen los mensajes a sus respectivos protocolos. Por simplicidad en esta version,
// redirigimos a los usuarios de esos modelos a usar un proxy OpenAI compatible o
// devolvemos un error de "no implementado".
// --------------------------------------------------------------------------------

// Stub para Anthropic (se implementaria la llamada a api.anthropic.com/v1/messages)
type AnthropicProvider struct{ apiKey, model string }
func NewAnthropicProvider(apiKey, model string) *AnthropicProvider { return &AnthropicProvider{apiKey, model} }
func (p *AnthropicProvider) Nombre() string { return "Anthropic Claude" }
func (p *AnthropicProvider) Complete(ctx context.Context, msgs []Message, tools []ToolDefinition) (*ChatResponse, error) {
	return nil, fmt.Errorf("el soporte nativo para Anthropic llegara en la proxima version. Por favor usa OpenAI, Groq o Ollama")
}

// Stub para Gemini (se implementaria la llamada a generativelanguage.googleapis.com)
type GeminiProvider struct{ apiKey, model string }
func NewGeminiProvider(apiKey, model string) *GeminiProvider { return &GeminiProvider{apiKey, model} }
func (p *GeminiProvider) Nombre() string { return "Google Gemini" }
func (p *GeminiProvider) Complete(ctx context.Context, msgs []Message, tools []ToolDefinition) (*ChatResponse, error) {
	return nil, fmt.Errorf("el soporte nativo para Gemini llegara en la proxima version. Por favor usa OpenAI, Groq o Ollama")
}

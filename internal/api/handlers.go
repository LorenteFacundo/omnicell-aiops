// Package api implementa los HTTP handlers del Gateway.
//
// Esta es la interfaz REST del sistema: expone todos los endpoints que
// el Dashboard y el MCP Server usan para interactuar con el Gateway.
//
// Endpoints disponibles:
//
//	POST /api/query         → Enviar una petición al sistema (el router decide la célula)
//	POST /api/store         → Guardar un nuevo registro en el sistema
//	GET  /api/metrics       → Obtener snapshot completo de métricas (usado por MCP y Dashboard)
//	GET  /api/cells         → Estado de todas las células
//	POST /api/cells/{id}/collapse   → Colapsar una célula (Chaos Engineering)
//	POST /api/cells/{id}/degrade    → Degradar una célula (simular alta carga)
//	POST /api/cells/{id}/recover    → Recuperar una célula colapsada
//	POST /api/rebalance     → Reasignar rangos de IDs entre células
//	POST /api/load-test     → Inyectar carga artificial de peticiones
package api

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/omnicell/aiops-engine/internal/ai"
	"github.com/omnicell/aiops-engine/internal/cache"
	"github.com/omnicell/aiops-engine/internal/cell"
	"github.com/omnicell/aiops-engine/internal/metrics"
	"github.com/omnicell/aiops-engine/internal/router"
)

// Handler agrupa todas las dependencias que los handlers necesitan.
// En lugar de variables globales, usamos dependency injection:
// cada handler recibe sus dependencias via esta struct.
type Handler struct {
	router    *router.Router
	cache     *cache.CacheOffHeap
	colector  *metrics.Colector
	aiEngine  *ai.AIEngine
}

// NuevoHandler crea un Handler con todas sus dependencias inyectadas.
func NuevoHandler(r *router.Router, c *cache.CacheOffHeap, col *metrics.Colector, ai *ai.AIEngine) *Handler {
	return &Handler{
		router:   r,
		cache:    c,
		colector: col,
		aiEngine: ai,
	}
}

// NuevoRouter configura el router HTTP con todos los endpoints y middlewares.
// Usamos chi porque es ligero, idiomático en Go, y compatible con net/http estándar.
func NuevoRouterHTTP(h *Handler) http.Handler {
	r := chi.NewRouter()

	// Middlewares de infraestructura que se aplican a TODOS los endpoints
	r.Use(middleware.Logger)    // Loguea cada request (método, path, status, duración)
	r.Use(middleware.Recoverer) // Captura panics y devuelve 500 en lugar de crashear
	r.Use(corsMiddleware)       // Permite que el Dashboard (web separada) llame a esta API

	// Grupo de rutas bajo el prefijo /api
	r.Route("/api", func(r chi.Router) {
		// Peticiones al sistema de almacenamiento
		r.Post("/query", h.handleQuery)   // Buscar un registro
		r.Post("/store", h.handleStore)   // Guardar un registro nuevo

		// Observabilidad (los endpoints más importantes para el demo)
		r.Get("/metrics", h.handleMetrics) // Snapshot de telemetría completo
		r.Get("/cells", h.handleGetCells)  // Estado de todas las células

		// Operaciones sobre células individuales (Chaos Engineering + Recuperación)
		r.Route("/cells/{cellID}", func(r chi.Router) {
			r.Post("/collapse", h.handleCollapseCell)  // Simular colapso
			r.Post("/degrade", h.handleDegradeCell)    // Simular degradación
			r.Post("/recover", h.handleRecoverCell)    // Recuperar célula
		})

		// Operaciones de rebalanceo (invocado por el agente de IA via MCP)
		r.Post("/rebalance", h.handleRebalance)

		// Load test para generar tráfico artificial (útil para el demo)
		r.Post("/load-test", h.handleLoadTest)

		// Endpoints del Motor AIOps Integrado
		r.Get("/ai/config", h.handleAIConfig)
		r.Post("/ai/config", h.handleAIConfig)
		r.Post("/ai/chat", h.handleAIChat)
		r.Get("/ai/history", h.handleAIHistory)
		r.Post("/ai/auto-healing", h.handleAIAutoHealing)
	})

	return r
}

// ---- Request/Response types ----

// FlexibleID acepta JSON string o number y lo normaliza a string.
// Evita fallos cuando el cliente envía "id": 2500000 en lugar de "id": "2500000".
type FlexibleID string

func (f *FlexibleID) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*f = ""
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*f = FlexibleID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("id debe ser string o número: %w", err)
	}
	*f = FlexibleID(n.String())
	return nil
}

func (f FlexibleID) String() string { return string(f) }

// QueryRequest es el body de POST /api/query
type QueryRequest struct {
	// ID es el identificador del registro buscado (string UUID o número)
	ID FlexibleID `json:"id"`

	// IDNumerico es el ID numérico para enrutamiento determinista.
	// Si es 0 y ID es numérico, se usa ID como routing key.
	IDNumerico int64 `json:"id_numerico,omitempty"`
}

// StoreRequest es el body de POST /api/store
type StoreRequest struct {
	// ID opcional: clave de almacenamiento. Si se omite, se genera un UUID.
	ID FlexibleID `json:"id,omitempty"`

	// IDNumerico determina a qué célula va este registro.
	// Debe estar dentro del rango de alguna célula registrada.
	// También se acepta el alias JSON "id_numerico" o, si ID es numérico y este
	// campo es 0, se deriva desde ID.
	IDNumerico int64 `json:"id_numerico"`

	Datos string `json:"datos"`
}

// RebalanceRequest es el body de POST /api/rebalance.
// El agente de IA construye este body cuando decide redistribuir el tráfico.
type RebalanceRequest struct {
	// NuevosRangos mapea el ID de cada célula a su nuevo rango de IDs.
	// Ejemplo: {"A": {"min": 0, "max": 1499999}, "B": {"min": 1500000, "max": 2999999}, "C": {"min": 3000000, "max": 3000000}}
	NuevosRangos map[string]router.RangoID `json:"nuevos_rangos"`
}

// LoadTestRequest es el body de POST /api/load-test
type LoadTestRequest struct {
	// CantidadRequests es cuántas peticiones inyectar
	CantidadRequests int `json:"cantidad_requests"`

	// CelulaObjetivo si está seteado, concentra la carga en esa célula específica.
	// Útil para simular un ataque o pico de tráfico enfocado.
	CelulaObjetivo string `json:"celula_objetivo,omitempty"`

	// Paralelo si es true, inyecta todas las peticiones en paralelo (más agresivo).
	Paralelo bool `json:"paralelo"`
}

// RespuestaOK es la respuesta genérica para operaciones exitosas.
type RespuestaOK struct {
	Mensaje string      `json:"mensaje"`
	Datos   interface{} `json:"datos,omitempty"`
}

// RespuestaError es la respuesta para errores.
type RespuestaError struct {
	Error string `json:"error"`
}

// ---- Handlers ----

// handleQuery procesa una búsqueda en el sistema celular.
// Si el request tiene IDNumerico (o un ID numérico), usa enrutamiento determinista.
// Si no, usa el Broadcast Gateway (scatter-gather).
func (h *Handler) handleQuery(w http.ResponseWriter, r *http.Request) {
	var req QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responderError(w, http.StatusBadRequest, "body JSON inválido: "+err.Error())
		return
	}

	idBusqueda := req.ID.String()
	if idBusqueda == "" {
		responderError(w, http.StatusBadRequest, "id es requerido")
		return
	}

	// Si no vino id_numerico pero el id es numérico, lo usamos para routing.
	idNumerico := req.IDNumerico
	if idNumerico == 0 {
		if n, err := strconv.ParseInt(idBusqueda, 10, 64); err == nil {
			idNumerico = n
		}
	}

	var registro *cell.Registro
	var err error
	var origen string

	if idNumerico > 0 {
		// Enrutamiento determinista: sabemos exactamente en qué célula buscar
		celula, errRuta := h.router.EnrutarPorID(idNumerico)
		if errRuta != nil {
			h.colector.RegistrarRequest(false)
			responderError(w, http.StatusServiceUnavailable, errRuta.Error())
			return
		}
		registro, err = celula.Obtener(idBusqueda)
		origen = "deterministic:" + celula.ID
	} else {
		// Broadcast Gateway: mandamos a todas las células y esperamos la primera respuesta
		resultado, errBroadcast := h.router.BroadcastGet(idBusqueda)
		if errBroadcast != nil {
			h.colector.RegistrarRequest(false)
			responderError(w, http.StatusServiceUnavailable, errBroadcast.Error())
			return
		}
		if resultado != nil {
			registro = resultado.Registro
			origen = "broadcast:" + resultado.Celula
		}
	}

	if err != nil {
		h.colector.RegistrarRequest(false)
		responderError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.colector.RegistrarRequest(true)

	if registro == nil {
		responderJSON(w, http.StatusNotFound, RespuestaOK{
			Mensaje: "registro no encontrado",
			Datos:   map[string]interface{}{"id": idBusqueda, "id_numerico": idNumerico, "origen": origen},
		})
		return
	}

	responderJSON(w, http.StatusOK, RespuestaOK{
		Mensaje: "encontrado",
		Datos:   map[string]interface{}{"registro": registro, "origen": origen, "id_numerico": idNumerico},
	})
}

// handleStore guarda un nuevo registro en el sistema.
// El IDNumerico determina en qué célula se almacena.
func (h *Handler) handleStore(w http.ResponseWriter, r *http.Request) {
	var req StoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responderError(w, http.StatusBadRequest, "body JSON inválido: "+err.Error())
		return
	}

	idNumerico := req.IDNumerico
	if idNumerico == 0 && req.ID.String() != "" {
		if n, err := strconv.ParseInt(req.ID.String(), 10, 64); err == nil {
			// Cliente envió solo "id" numérico: usarlo como routing key
			// y generar UUID para la clave de almacenamiento.
			idNumerico = n
		}
	}

	if idNumerico <= 0 {
		responderError(w, http.StatusBadRequest, "id_numerico es requerido y debe ser > 0")
		return
	}

	// Clave de almacenamiento: ID cliente (si no es solo el routing number) o UUID
	id := req.ID.String()
	if id == "" || id == strconv.FormatInt(idNumerico, 10) {
		id = uuid.New().String()
	}

	registro := &cell.Registro{
		ID:       id,
		Datos:    req.Datos,
		CreadoEn: time.Now(),
	}

	// Enrutamos el registro a la célula correcta según su ID numérico
	celula, err := h.router.EnrutarPorID(idNumerico)
	if err != nil {
		h.colector.RegistrarRequest(false)
		responderError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	if err := celula.Guardar(registro); err != nil {
		h.colector.RegistrarRequest(false)
		responderError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// También guardamos en el caché off-heap para lecturas futuras ultra-rápidas
	claveCache := fmt.Sprintf("reg:%s", id)
	_ = h.cache.Guardar(claveCache, registro) // Ignoramos error de caché (es best-effort)

	h.colector.RegistrarRequest(true)
	responderJSON(w, http.StatusCreated, RespuestaOK{
		Mensaje: "registro guardado exitosamente",
		Datos: map[string]interface{}{
			"id":          id,
			"id_numerico": idNumerico,
			"celula":      celula.ID,
		},
	})
}

// handleMetrics devuelve el snapshot completo de telemetría del sistema.
// Este es el endpoint más importante para el Demo:
// - El Dashboard lo llama cada 1 segundo para actualizar la visualización
// - El MCP Server lo llama cuando la IA ejecuta "query_cell_latencies"
func (h *Handler) handleMetrics(w http.ResponseWriter, r *http.Request) {
	snapshot := h.colector.ObtenerSnapshot()
	responderJSON(w, http.StatusOK, snapshot)
}

// handleGetCells devuelve el estado actual de todas las células y sus rangos.
func (h *Handler) handleGetCells(w http.ResponseWriter, r *http.Request) {
	celulas := h.router.ObtenerCelulas()
	rangos := h.router.ObtenerRangos()

	type infoCelula struct {
		ID       string         `json:"id"`
		Estado   string         `json:"estado"`
		RangoMin int64          `json:"rango_min"`
		RangoMax int64          `json:"rango_max"`
		Registros int           `json:"registros"`
	}

	infos := make([]infoCelula, 0, len(celulas))
	for _, c := range celulas {
		rango := rangos[c.ID]
		infos = append(infos, infoCelula{
			ID:        c.ID,
			Estado:    string(c.ObtenerEstado()),
			RangoMin:  rango.Min,
			RangoMax:  rango.Max,
			Registros: c.CantidadRegistros(),
		})
	}

	responderJSON(w, http.StatusOK, RespuestaOK{Datos: infos})
}

// handleCollapseCell colapsa una célula, simulando un fallo catastrófico de base de datos.
// Este es el "disparo de pistola" del demo: el agente de IA lo invoca via
// la tool "simulate_bulkhead_collapse" para verificar la resiliencia del sistema.
func (h *Handler) handleCollapseCell(w http.ResponseWriter, r *http.Request) {
	cellID := chi.URLParam(r, "cellID")

	celula, existe := h.router.ObtenerCelula(cellID)
	if !existe {
		responderError(w, http.StatusNotFound, fmt.Sprintf("célula '%s' no existe", cellID))
		return
	}

	celula.SimularColapso()

	responderJSON(w, http.StatusOK, RespuestaOK{
		Mensaje: fmt.Sprintf("Celula %s colapsada exitosamente. El sistema esta en modo de recuperacion.", cellID),
	})
}

// handleDegradeCell degrada una célula (alta latencia, aún responde).
func (h *Handler) handleDegradeCell(w http.ResponseWriter, r *http.Request) {
	cellID := chi.URLParam(r, "cellID")

	celula, existe := h.router.ObtenerCelula(cellID)
	if !existe {
		responderError(w, http.StatusNotFound, fmt.Sprintf("célula '%s' no existe", cellID))
		return
	}

	celula.SimularDegradacion()

	responderJSON(w, http.StatusOK, RespuestaOK{
		Mensaje: fmt.Sprintf("Celula %s degradada. Respondiendo con alta latencia.", cellID),
	})
}

// handleRecoverCell recupera una célula, volviéndola al estado saludable.
// En el demo, la IA invoca esto después de haber rebalanceado el tráfico.
func (h *Handler) handleRecoverCell(w http.ResponseWriter, r *http.Request) {
	cellID := chi.URLParam(r, "cellID")

	celula, existe := h.router.ObtenerCelula(cellID)
	if !existe {
		responderError(w, http.StatusNotFound, fmt.Sprintf("célula '%s' no existe", cellID))
		return
	}

	celula.Recuperar()

	responderJSON(w, http.StatusOK, RespuestaOK{
		Mensaje: fmt.Sprintf("Celula %s recuperada. Volviendo a estado saludable.", cellID),
	})
}

// handleRebalance redistribuye los rangos de IDs entre células.
// Este es el endpoint de "cirugía en caliente": el agente de IA lo llama
// cuando detecta una célula colapsada y quiere redirigir su tráfico a las sanas.
// El sistema no se detiene durante el rebalanceo (zero-downtime).
func (h *Handler) handleRebalance(w http.ResponseWriter, r *http.Request) {
	var req RebalanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responderError(w, http.StatusBadRequest, "body JSON inválido: "+err.Error())
		return
	}

	if len(req.NuevosRangos) == 0 {
		responderError(w, http.StatusBadRequest, "nuevos_rangos no puede estar vacío")
		return
	}

	if err := h.router.Rebalancear(req.NuevosRangos); err != nil {
		responderError(w, http.StatusBadRequest, "error al rebalancear: "+err.Error())
		return
	}

	responderJSON(w, http.StatusOK, RespuestaOK{
		Mensaje: "rangos rebalanceados exitosamente. El trafico ahora fluye a las celulas sanas.",
		Datos:   req.NuevosRangos,
	})
}

// -----------------------------------------------------------------------------
// ENDPOINTS AIOps / Motor de IA
// -----------------------------------------------------------------------------

func (h *Handler) handleAIConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		config := h.aiEngine.GetConfig()
		responderJSON(w, http.StatusOK, config)
		return
	}

	if r.Method == http.MethodPost {
		var req ai.Config
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			responderJSON(w, http.StatusBadRequest, RespuestaError{Error: "JSON invalido"})
			return
		}
		
		h.aiEngine.SetConfig(req)
		responderJSON(w, http.StatusOK, RespuestaOK{Mensaje: "Configuracion IA guardada"})
		return
	}

	http.Error(w, "Metodo no permitido", http.StatusMethodNotAllowed)
}

func (h *Handler) handleAIChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Metodo no permitido", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Mensaje string `json:"mensaje"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responderJSON(w, http.StatusBadRequest, RespuestaError{Error: "JSON invalido"})
		return
	}

	if !h.aiEngine.EstaConfigurado() {
		responderJSON(w, http.StatusBadRequest, RespuestaError{Error: "El motor de IA no esta configurado. Falta API Key o Proveedor."})
		return
	}

	ctx := r.Context()
	result, err := h.aiEngine.Chat(ctx, req.Mensaje)
	if err != nil {
		responderJSON(w, http.StatusInternalServerError, RespuestaError{Error: err.Error()})
		return
	}

	responderJSON(w, http.StatusOK, result)
}

func (h *Handler) handleAIHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Metodo no permitido", http.StatusMethodNotAllowed)
		return
	}

	historia := h.aiEngine.GetHistoria()
	responderJSON(w, http.StatusOK, historia)
}

func (h *Handler) handleAIAutoHealing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Metodo no permitido", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Activo bool `json:"activo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responderJSON(w, http.StatusBadRequest, RespuestaError{Error: "JSON invalido"})
		return
	}

	h.aiEngine.SetAutoHealing(req.Activo)
	estado := "desactivado"
	if req.Activo {
		estado = "activado"
	}
	
	responderJSON(w, http.StatusOK, RespuestaOK{Mensaje: "Guardian Auto-Healing " + estado})
}

// handleLoadTest inyecta tráfico artificial para estresar el sistema.
// Útil para demostrar el colapso de una célula bajo carga extrema.
func (h *Handler) handleLoadTest(w http.ResponseWriter, r *http.Request) {
	var req LoadTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responderError(w, http.StatusBadRequest, "body JSON inválido: "+err.Error())
		return
	}

	if req.CantidadRequests <= 0 || req.CantidadRequests > 100_000 {
		responderError(w, http.StatusBadRequest, "cantidad_requests debe estar entre 1 y 100.000")
		return
	}

	// Determinamos los rangos de IDs numéricos a usar para el load test
	rangos := h.router.ObtenerRangos()
	rangoSeleccionado, tieneRango := rangos[req.CelulaObjetivo]

	ejecutarRequest := func() {
		var idNum int64

		if tieneRango {
			span := rangoSeleccionado.Max - rangoSeleccionado.Min + 1
			if span <= 0 {
				// Rango drenado (ej. célula colapsada sin tráfico): cuenta como fallo
				h.colector.RegistrarRequest(false)
				return
			}
			// Concentramos el tráfico en la célula objetivo
			idNum = rangoSeleccionado.Min + rand.Int63n(span)
		} else {
			// Distribuimos el tráfico uniformemente entre todas las células
			idNum = rand.Int63n(3_000_000) // Rango total del sistema
		}

		// Enrutamos y ejecutamos una lectura real para actualizar contadores por célula
		celula, errRuta := h.router.EnrutarPorID(idNum)
		if celula == nil {
			h.colector.RegistrarRequest(false)
			return
		}
		_, errOp := celula.Obtener(fmt.Sprintf("lt-%d", idNum))
		h.colector.RegistrarRequest(errRuta == nil && errOp == nil)
	}

	if req.Paralelo {
		// Modo agresivo: lanzamos todas las peticiones en goroutines paralelas
		var wg sync.WaitGroup
		for i := 0; i < req.CantidadRequests; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ejecutarRequest()
			}()
		}
		wg.Wait()
	} else {
		// Modo secuencial: más controlado
		for i := 0; i < req.CantidadRequests; i++ {
			ejecutarRequest()
		}
	}

	responderJSON(w, http.StatusOK, RespuestaOK{
		Mensaje: fmt.Sprintf("%d requests inyectados exitosamente", req.CantidadRequests),
		Datos:   map[string]interface{}{"paralelo": req.Paralelo, "celula_objetivo": req.CelulaObjetivo},
	})
}

// ---- Helpers ----

// responderJSON serializa la respuesta a JSON y la escribe al ResponseWriter.
func responderJSON(w http.ResponseWriter, statusCode int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

// responderError escribe una respuesta de error JSON estandarizada.
func responderError(w http.ResponseWriter, statusCode int, mensaje string) {
	responderJSON(w, statusCode, RespuestaError{Error: mensaje})
}

// corsMiddleware agrega los headers CORS necesarios para que el Dashboard
// (servido desde un puerto diferente) pueda llamar a esta API.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		// Respondemos inmediatamente a las preflight requests de OPTIONS
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}



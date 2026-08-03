// Package metrics recopila y expone telemetría del sistema en tiempo real.
//
// La observabilidad es el corazón de OmniCell: sin datos, el agente de IA
// no puede diagnosticar nada. Este paquete actúa como los "sensores" del sistema,
// midiendo continuamente el estado de las células, el caché y el rendimiento global.
//
// Las métricas que recopilamos se basan en las mismas que usa Mercado Libre:
// - Latencia p99 por célula (el indicador más honesto de rendimiento bajo carga)
// - Pausas del Garbage Collector (el problema que BigCache elimina)
// - Rate de requests/segundo (para detectar picos de tráfico)
// - Estado de salud de cada célula (para detección automática de fallos)
package metrics

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/omnicell/aiops-engine/internal/cache"
	"github.com/omnicell/aiops-engine/internal/cell"
)

// EstadoCelula es el snapshot de métricas de una célula individual.
// El agente de IA recibe esta estructura cuando ejecuta "query_cell_latencies".
type EstadoCelula struct {
	ID                 string        `json:"id"`
	Estado             string        `json:"estado"`
	RangoMin           int64         `json:"rango_min"`
	RangoMax           int64         `json:"rango_max"`
	LatenciaP99Ms      float64       `json:"latencia_p99_ms"`
	LatenciaPromedioMs float64       `json:"latencia_promedio_ms"`
	TotalPeticiones    int64         `json:"total_peticiones"`
	PeticionesFallidas int64         `json:"peticiones_fallidas"`
	CantidadRegistros  int           `json:"cantidad_registros"`
	Alerta             bool          `json:"alerta"` // true si latencia > umbral o está colapsada
	MensajeAlerta      string        `json:"mensaje_alerta,omitempty"`
}

// EstadoGC contiene las estadísticas del Garbage Collector de Go.
// Estas métricas demuestran el impacto del off-heap caching:
// con BigCache, las pausas de GC deben ser casi cero.
type EstadoGC struct {
	// NumCiclos es cuántas veces corrió el GC desde que inició el programa.
	NumCiclos uint32 `json:"num_ciclos"`

	// UltimaPausaMs es cuánto tiempo tardó el último ciclo de GC en milisegundos.
	// Con BigCache, este valor debe ser < 1ms. Sin él, puede ser > 100ms.
	UltimaPausaMs float64 `json:"ultima_pausa_ms"`

	// PausaTotalMs es el tiempo total acumulado que el GC ha pausado el programa.
	PausaTotalMs float64 `json:"pausa_total_ms"`

	// MemHeapMB es cuánta RAM del heap está en uso actualmente.
	MemHeapMB float64 `json:"mem_heap_mb"`

	// AllocacionesPorOp es el promedio de asignaciones de memoria por operación.
	// Mercado Libre redujo esto de 25.500 a 900 con sus optimizaciones.
	AllocacionesTotales uint64 `json:"allocaciones_totales"`
}

// EstadoCache contiene métricas del caché off-heap.
type EstadoCache struct {
	TasaHitPorcentaje   float64 `json:"tasa_hit_porcentaje"`
	CantidadEntradas    int     `json:"cantidad_entradas"`
	CapacidadUsadaMB    float64 `json:"capacidad_usada_mb"`
}

// SnapshotSistema es el estado completo del sistema en un momento dado.
// Esta es la estructura principal que el agente de IA analiza para tomar decisiones.
type SnapshotSistema struct {
	// Timestamp de cuándo se tomó esta medición
	Timestamp time.Time `json:"timestamp"`

	// RequestsPorSegundo es el throughput actual del sistema
	RequestsPorSegundo float64 `json:"requests_por_segundo"`

	// TotalRequests es el contador global desde que inició el programa
	TotalRequests int64 `json:"total_requests"`

	// RequestsExitosos y RequestsFallidos para calcular tasa de error
	RequestsExitosos int64 `json:"requests_exitosos"`
	RequestsFallidos int64 `json:"requests_fallidos"`

	// Celulas contiene el estado detallado de cada célula
	Celulas []EstadoCelula `json:"celulas"`

	// GC contiene las estadísticas del Garbage Collector
	GC EstadoGC `json:"gc"`

	// Cache contiene las estadísticas del caché off-heap
	Cache EstadoCache `json:"cache"`

	// HayCelulasColapsadas es un flag de conveniencia para que la IA
	// pueda detectar rápidamente si hay un problema crítico.
	HayCelulasColapsadas bool `json:"hay_celulas_colapsadas"`
}

// Colector recopila métricas del sistema de forma continua.
// Es el "cerebro sensorial" del Gateway: observa y mide todo.
type Colector struct {
	mu sync.RWMutex

	// Referencia a las células para leer sus estadísticas
	celulas map[string]*cell.Celula

	// Referencia al caché para leer sus estadísticas
	offheapCache *cache.CacheOffHeap

	// Contadores globales de requests
	totalRequests    atomic.Int64
	requestsExitosos atomic.Int64
	requestsFallidos atomic.Int64

	// Para calcular requests/segundo, guardamos el último snapshot
	ultimoSnapshotRequests int64
	ultimoSnapshotTiempo   time.Time

	// Umbral de alerta para latencia p99 en milisegundos.
	// Si una célula supera este valor, se marca con alerta.
	umbralLatenciaAlertaMs float64
}

// NuevoColector crea un nuevo colector de métricas.
//
// Parámetros:
//   - celulas: mapa de todas las células del sistema
//   - offheapCache: el caché off-heap del sistema
//   - umbralLatenciaAlertaMs: latencia p99 máxima aceptable (en ms).
//     Si una célula supera este umbral, se genera una alerta.
func NuevoColector(
	celulas map[string]*cell.Celula,
	offheapCache *cache.CacheOffHeap,
	umbralLatenciaAlertaMs float64,
) *Colector {
	return &Colector{
		celulas:                celulas,
		offheapCache:           offheapCache,
		ultimoSnapshotTiempo:   time.Now(),
		umbralLatenciaAlertaMs: umbralLatenciaAlertaMs,
	}
}

// RegistrarRequest registra una petición al sistema para el cálculo de throughput.
// Debe llamarse desde los HTTP handlers por cada petición recibida.
func (c *Colector) RegistrarRequest(exitoso bool) {
	c.totalRequests.Add(1)
	if exitoso {
		c.requestsExitosos.Add(1)
	} else {
		c.requestsFallidos.Add(1)
	}
}

// ObtenerSnapshot genera un snapshot completo del estado del sistema.
// Este método es lo que el agente de IA llama (via MCP) para diagnosticar problemas.
//
// Es thread-safe y puede llamarse desde múltiples goroutines sin problemas.
func (c *Colector) ObtenerSnapshot() SnapshotSistema {
	c.mu.Lock()
	ahora := time.Now()
	duracion := ahora.Sub(c.ultimoSnapshotTiempo).Seconds()
	totalActual := c.totalRequests.Load()

	// Calculamos requests/segundo desde el último snapshot
	requestsPorSegundo := 0.0
	if duracion > 0 {
		requestsPorSegundo = float64(totalActual-c.ultimoSnapshotRequests) / duracion
	}

	// Actualizamos el baseline para el próximo cálculo
	c.ultimoSnapshotRequests = totalActual
	c.ultimoSnapshotTiempo = ahora
	c.mu.Unlock()

	// Recopilamos el estado de cada célula
	estadosCelulas := c.recopilarEstadosCelulas()

	// Verificamos si hay células colapsadas (flag de conveniencia para la IA)
	hayCelulasColapsadas := false
	for _, ec := range estadosCelulas {
		if ec.Estado == string(cell.EstadoColapsado) {
			hayCelulasColapsadas = true
			break
		}
	}

	return SnapshotSistema{
		Timestamp:            ahora,
		RequestsPorSegundo:   requestsPorSegundo,
		TotalRequests:        totalActual,
		RequestsExitosos:     c.requestsExitosos.Load(),
		RequestsFallidos:     c.requestsFallidos.Load(),
		Celulas:              estadosCelulas,
		GC:                   recopilarEstadoGC(),
		Cache:                c.recopilarEstadoCache(),
		HayCelulasColapsadas: hayCelulasColapsadas,
	}
}

// recopilarEstadosCelulas construye la lista de estados de todas las células.
func (c *Colector) recopilarEstadosCelulas() []EstadoCelula {
	estados := make([]EstadoCelula, 0, len(c.celulas))

	for _, celula := range c.celulas {
		stats := celula.ObtenerEstadisticas()
		estado := celula.ObtenerEstado()

		latenciaP99Ms := float64(stats.LatenciaP99.Microseconds()) / 1000.0
		latenciaPromedioMs := float64(stats.LatenciaPromedio.Microseconds()) / 1000.0

		// Determinamos si hay alerta: célula colapsada O latencia p99 muy alta
		alerta := false
		mensajeAlerta := ""

		if estado == cell.EstadoColapsado {
			alerta = true
			mensajeAlerta = "CRÍTICO: Célula colapsada — sin respuesta"
		} else if estado == cell.EstadoDegradado {
			alerta = true
			mensajeAlerta = "ADVERTENCIA: Célula degradada — latencia elevada"
		} else if latenciaP99Ms > c.umbralLatenciaAlertaMs {
			alerta = true
			mensajeAlerta = "ADVERTENCIA: Latencia p99 supera el umbral de alerta"
		}

		estados = append(estados, EstadoCelula{
			ID:                 celula.ID,
			Estado:             string(estado),
			RangoMin:           celula.RangoMin,
			RangoMax:           celula.RangoMax,
			LatenciaP99Ms:      latenciaP99Ms,
			LatenciaPromedioMs: latenciaPromedioMs,
			TotalPeticiones:    stats.TotalPeticiones,
			PeticionesFallidas: stats.PeticionesFallidas,
			CantidadRegistros:  celula.CantidadRegistros(),
			Alerta:             alerta,
			MensajeAlerta:      mensajeAlerta,
		})
	}

	return estados
}

// recopilarEstadoGC lee las estadísticas del Garbage Collector de Go usando
// el paquete runtime estándar. Estas son las métricas que demuestran el beneficio
// de BigCache: con el caché off-heap, el GC tiene mucho menos trabajo que hacer.
func recopilarEstadoGC() EstadoGC {
	// runtime.MemStats contiene TODO sobre el uso de memoria y el GC en Go.
	// Es como un profiler integrado en el runtime.
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// La última pausa de GC está en nanosegundos en PauseNs circular buffer.
	// NumGC nos dice cuántos ciclos hubo; la última pausa está en PauseNs[(NumGC+255)%256].
	ultimaPausaNs := uint64(0)
	if memStats.NumGC > 0 {
		ultimaPausaNs = memStats.PauseNs[(memStats.NumGC+255)%256]
	}

	return EstadoGC{
		NumCiclos:           memStats.NumGC,
		UltimaPausaMs:       float64(ultimaPausaNs) / 1e6,          // ns → ms
		PausaTotalMs:        float64(memStats.PauseTotalNs) / 1e6,   // ns → ms
		MemHeapMB:           float64(memStats.HeapInuse) / 1024 / 1024, // bytes → MB
		AllocacionesTotales: memStats.TotalAlloc,
	}
}

// recopilarEstadoCache obtiene las estadísticas del caché off-heap.
func (c *Colector) recopilarEstadoCache() EstadoCache {
	stats := c.offheapCache.ObtenerEstadisticas()
	return EstadoCache{
		TasaHitPorcentaje: stats.TasaHit,
		CantidadEntradas:  stats.CantidadEntradas,
		CapacidadUsadaMB:  float64(stats.CapacidadUsadaBytes) / 1024 / 1024,
	}
}

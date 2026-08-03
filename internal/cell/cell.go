// Package cell simula una "célula" de base de datos independiente.
//
// En la arquitectura celular real (como la de Mercado Envíos), cada célula
// es un compartimento hermético con su propio poder computacional y almacenamiento.
// Si una célula colapsa, las demás siguen funcionando sin interrupciones.
//
// En este simulador, cada célula es un mapa en memoria protegido por un mutex,
// lo que imita el comportamiento de una base de datos independiente.
package cell

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Estado representa el estado de salud de una célula.
// Las células pueden estar sanas, degradadas o completamente colapsadas.
type Estado string

const (
	// EstadoSaludable significa que la célula opera normalmente.
	EstadoSaludable Estado = "healthy"

	// EstadoDegradado significa que la célula responde lento (alta latencia).
	EstadoDegradado Estado = "degraded"

	// EstadoColapsado significa que la célula no responde (simula un fallo catastrófico).
	EstadoColapsado Estado = "collapsed"
)

// Registro es la unidad de dato que vive dentro de una célula.
type Registro struct {
	ID        string    `json:"id"`
	Datos     string    `json:"datos"`
	CreadoEn  time.Time `json:"creado_en"`
}

// Estadisticas contiene métricas de rendimiento de una célula.
// Estas métricas son las que el agente de IA va a consultar via MCP
// para diagnosticar problemas en el sistema.
type Estadisticas struct {
	TotalPeticiones   int64         `json:"total_peticiones"`
	PeticionesExitosas int64        `json:"peticiones_exitosas"`
	PeticionesFallidas int64        `json:"peticiones_fallidas"`
	LatenciaPromedio  time.Duration `json:"latencia_promedio_ns"`
	LatenciaP99       time.Duration `json:"latencia_p99_ns"`
	UltimoError       string        `json:"ultimo_error,omitempty"`
}

// Celula representa un compartimento hermético e independiente de almacenamiento.
// Cada célula tiene su propio mapa de datos, su estado y sus métricas.
type Celula struct {
	// ID único de la célula (ej: "A", "B", "C")
	ID string

	// RangoMin y RangoMax definen qué IDs numéricos pertenecen a esta célula.
	// Por ejemplo: Célula A = IDs 0 a 999.999
	RangoMin int64
	RangoMax int64

	// mu protege el acceso concurrente al mapa de datos.
	// En Go, los maps NO son thread-safe, así que usamos un mutex de lectura/escritura.
	// RWMutex permite múltiples lecturas simultáneas pero bloquea en escrituras.
	mu sync.RWMutex

	// datos es el almacenamiento principal de la célula (simula una base de datos).
	datos map[string]*Registro

	// estado es el estado de salud actual de la célula.
	// Usamos atomic.Value para leer/escribir el estado de forma thread-safe sin mutex.
	estado atomic.Value

	// latenciasMs almacena las últimas N latencias para calcular percentiles.
	latenciasNs   []int64
	latenciasMu   sync.Mutex

	// Contadores atómicos para estadísticas (atomic evita locks para simples incrementos).
	totalPeticiones    atomic.Int64
	peticionesExitosas atomic.Int64
	peticionesFallidas atomic.Int64
}

// NuevaCelula crea e inicializa una nueva célula con su rango de IDs asignado.
func NuevaCelula(id string, rangoMin, rangoMax int64) *Celula {
	c := &Celula{
		ID:       id,
		RangoMin: rangoMin,
		RangoMax: rangoMax,
		datos:    make(map[string]*Registro),
		// Guardamos las últimas 1000 latencias para calcular percentiles
		latenciasNs: make([]int64, 0, 1000),
	}

	// Establecemos el estado inicial como saludable
	c.estado.Store(EstadoSaludable)

	return c
}

// ObtenerEstado devuelve el estado de salud actual de la célula.
// Es thread-safe gracias al uso de atomic.Value.
func (c *Celula) ObtenerEstado() Estado {
	return c.estado.Load().(Estado)
}

// EstaDisponible devuelve true si la célula puede procesar peticiones.
func (c *Celula) EstaDisponible() bool {
	return c.ObtenerEstado() != EstadoColapsado
}

// Guardar almacena un registro en la célula.
// Retorna error si la célula está colapsada.
func (c *Celula) Guardar(reg *Registro) error {
	// Registramos el inicio para calcular latencia
	inicio := time.Now()
	c.totalPeticiones.Add(1)

	// Si la célula está colapsada, rechazamos la petición
	if !c.EstaDisponible() {
		c.peticionesFallidas.Add(1)
		return fmt.Errorf("célula %s no disponible: estado=%s", c.ID, c.ObtenerEstado())
	}

	// Si está degradada, simulamos latencia alta (como una DB saturada)
	if c.ObtenerEstado() == EstadoDegradado {
		time.Sleep(50 * time.Millisecond)
	}

	// Lock de escritura: nadie más puede leer ni escribir mientras guardamos
	c.mu.Lock()
	c.datos[reg.ID] = reg
	c.mu.Unlock()

	// Registramos la latencia de esta operación
	c.registrarLatencia(time.Since(inicio))
	c.peticionesExitosas.Add(1)

	return nil
}

// Obtener recupera un registro por su ID.
// Retorna (nil, nil) si el registro no existe.
// Retorna (nil, error) si la célula está colapsada.
func (c *Celula) Obtener(id string) (*Registro, error) {
	inicio := time.Now()
	c.totalPeticiones.Add(1)

	if !c.EstaDisponible() {
		c.peticionesFallidas.Add(1)
		return nil, fmt.Errorf("célula %s no disponible: estado=%s", c.ID, c.ObtenerEstado())
	}

	if c.ObtenerEstado() == EstadoDegradado {
		time.Sleep(50 * time.Millisecond)
	}

	// RLock de lectura: múltiples goroutines pueden leer simultáneamente
	c.mu.RLock()
	reg, existe := c.datos[id]
	c.mu.RUnlock()

	c.registrarLatencia(time.Since(inicio))

	if !existe {
		// No es un error, simplemente no existe en esta célula
		return nil, nil
	}

	c.peticionesExitosas.Add(1)
	return reg, nil
}

// SimularColapso pone la célula en estado colapsado, imitando un fallo catastrófico
// de base de datos (como una saturación de CPU/IO que mata el proceso).
// Este es el método que el agente de IA invoca via la tool "simulate_bulkhead_collapse".
func (c *Celula) SimularColapso() {
	c.estado.Store(EstadoColapsado)
}

// SimularDegradacion pone la célula en estado degradado (alta latencia, aún responde).
// Útil para simular una DB bajo alta carga antes del colapso total.
func (c *Celula) SimularDegradacion() {
	c.estado.Store(EstadoDegradado)
}

// Recuperar vuelve la célula a estado saludable.
// En un sistema real, esto implicaría reiniciar el proceso, liberar locks, etc.
func (c *Celula) Recuperar() {
	c.estado.Store(EstadoSaludable)
}

// ObtenerEstadisticas devuelve un snapshot de las métricas actuales de la célula.
// Este es el dato principal que el agente de IA analiza para detectar anomalías.
func (c *Celula) ObtenerEstadisticas() Estadisticas {
	c.latenciasMu.Lock()
	defer c.latenciasMu.Unlock()

	stats := Estadisticas{
		TotalPeticiones:    c.totalPeticiones.Load(),
		PeticionesExitosas: c.peticionesExitosas.Load(),
		PeticionesFallidas: c.peticionesFallidas.Load(),
	}

	// Calculamos percentiles a partir de las latencias registradas
	if len(c.latenciasNs) > 0 {
		stats.LatenciaPromedio = calcularPromedio(c.latenciasNs)
		stats.LatenciaP99 = calcularP99(c.latenciasNs)
	}

	return stats
}

// CantidadRegistros devuelve cuántos registros almacena la célula.
// Thread-safe gracias al RLock.
func (c *Celula) CantidadRegistros() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.datos)
}

// registrarLatencia agrega una medición de latencia al historial circular.
// Mantenemos solo las últimas 1000 mediciones para no consumir memoria infinita.
func (c *Celula) registrarLatencia(d time.Duration) {
	c.latenciasMu.Lock()
	defer c.latenciasMu.Unlock()

	ns := d.Nanoseconds()

	// Si el buffer está lleno, rotamos (eliminamos la más vieja)
	if len(c.latenciasNs) >= 1000 {
		c.latenciasNs = c.latenciasNs[1:]
	}
	c.latenciasNs = append(c.latenciasNs, ns)
}

// calcularPromedio calcula la latencia promedio en nanosegundos.
func calcularPromedio(latencias []int64) time.Duration {
	if len(latencias) == 0 {
		return 0
	}
	var suma int64
	for _, l := range latencias {
		suma += l
	}
	return time.Duration(suma / int64(len(latencias)))
}

// calcularP99 calcula el percentil 99 de latencia sin ordenar el slice completo.
// El p99 significa: "el 99% de las peticiones fueron más rápidas que este valor".
// En sistemas de producción, el p99 es la métrica clave porque revela los outliers.
func calcularP99(latencias []int64) time.Duration {
	if len(latencias) == 0 {
		return 0
	}

	// Hacemos una copia para no modificar el original
	copia := make([]int64, len(latencias))
	copy(copia, latencias)

	// Ordenamiento burbuja simple (suficiente para 1000 elementos)
	// En producción usaríamos sort.Slice o un algoritmo de selección
	for i := 0; i < len(copia)-1; i++ {
		for j := 0; j < len(copia)-i-1; j++ {
			if copia[j] > copia[j+1] {
				copia[j], copia[j+1] = copia[j+1], copia[j]
			}
		}
	}

	// El p99 está en la posición 99% del slice ordenado
	indiceP99 := int(float64(len(copia)) * 0.99)
	if indiceP99 >= len(copia) {
		indiceP99 = len(copia) - 1
	}

	return time.Duration(copia[indiceP99])
}

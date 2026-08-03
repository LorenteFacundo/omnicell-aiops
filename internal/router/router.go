// Package router implementa el enrutador celular del Gateway.
//
// Este router resuelve el problema central de la arquitectura celular:
// ¿A cuál de las N células independientes le mando esta petición?
//
// Hay dos estrategias:
//
//  1. Enrutamiento Determinista: Si la petición tiene un ID numérico, calculamos
//     la célula por su rango. Ej: ID=500.000 → Célula A (rango 0-999.999).
//     Es O(1) y extremadamente rápido.
//
//  2. Broadcast Gateway (scatter-gather): Si la petición no tiene ID (tráfico
//     heredado sin routing key), la mandamos a TODAS las células en paralelo
//     y nos quedamos con la primera respuesta no-vacía. Garantiza compatibilidad
//     hacia atrás con APIs viejas.
package router

import (
	"fmt"
	"sync"

	"github.com/omnicell/aiops-engine/internal/cell"
)

// RangoID define el rango de IDs numéricos que pertenecen a una célula.
// Esta es la estructura que el agente de IA modifica cuando ejecuta
// la tool "rebalance_id_ranges" para redirigir tráfico de una célula colapsada.
type RangoID struct {
	Min int64 `json:"min"`
	Max int64 `json:"max"`
}

// ResultadoBroadcast contiene el resultado de una consulta broadcast (scatter-gather).
// Cuando se manda la petición a todas las células, la primera que responde gana.
type ResultadoBroadcast struct {
	// Celula es el ID de la célula que respondió con datos
	Celula string
	// Registro es el dato encontrado (nil si ninguna célula lo tenía)
	Registro *cell.Registro
}

// Router administra el enrutamiento de peticiones entre las células.
// Mantiene un mapa de rangos de IDs que puede ser reconfigurado en caliente
// (sin detener el sistema) cuando la IA ejecuta un rebalanceo.
type Router struct {
	// mu protege el mapa de rangos durante rebalanceos.
	// Usamos RWMutex porque las lecturas (enrutamiento) son mucho más frecuentes
	// que las escrituras (rebalanceos).
	mu sync.RWMutex

	// celulas es el mapa de todas las células registradas, indexadas por su ID.
	celulas map[string]*cell.Celula

	// rangos es el mapa de rangos de IDs, indexado por ID de célula.
	// Ejemplo: {"A": {Min: 0, Max: 999999}, "B": {Min: 1000000, Max: 1999999}}
	rangos map[string]RangoID

	// ordenCelulas mantiene el orden de registro de las células
	// para el broadcast determinístico (siempre mismo orden).
	ordenCelulas []string
}

// NuevoRouter crea un nuevo router con las células provistas.
// Las células deben estar pre-configuradas con sus rangos iniciales.
func NuevoRouter(celulas []*cell.Celula) *Router {
	r := &Router{
		celulas:      make(map[string]*cell.Celula),
		rangos:       make(map[string]RangoID),
		ordenCelulas: make([]string, 0, len(celulas)),
	}

	// Registramos cada célula y su rango de IDs inicial
	for _, c := range celulas {
		r.celulas[c.ID] = c
		r.rangos[c.ID] = RangoID{Min: c.RangoMin, Max: c.RangoMax}
		r.ordenCelulas = append(r.ordenCelulas, c.ID)
	}

	return r
}

// EnrutarPorID enruta una petición de forma determinista usando el ID numérico.
// Si el ID cae en el rango de una célula, la devuelve directamente.
// Si la célula está colapsada, retorna error inmediatamente.
//
// Este método es O(N) donde N es el número de células (normalmente 3-5),
// lo que en práctica es O(1) constante.
func (r *Router) EnrutarPorID(idNumerico int64) (*cell.Celula, error) {
	// Lock de lectura: múltiples goroutines pueden enrutar en paralelo
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Buscamos qué célula tiene el rango que incluye este ID
	for _, idCelula := range r.ordenCelulas {
		rango := r.rangos[idCelula]
		if idNumerico >= rango.Min && idNumerico <= rango.Max {
			celula := r.celulas[idCelula]

			// Si la célula está colapsada, fallamos rápido (fail-fast)
			if !celula.EstaDisponible() {
				// Devolvemos la célula junto al error para que load-test / métricas
				// puedan registrar el fallo en los contadores de esa célula.
				return celula, fmt.Errorf("célula %s colapsada (ID %d cae en su rango)", idCelula, idNumerico)
			}

			return celula, nil
		}
	}

	// Si ninguna célula tiene ese rango, el ID no pertenece al sistema
	return nil, fmt.Errorf("ID %d no pertenece a ningún rango celular", idNumerico)
}

// BroadcastGet busca un registro en TODAS las células simultáneamente.
// Es el patrón scatter-gather para peticiones sin routing key conocida.
//
// Funcionamiento:
//   - Lanzamos una goroutine por cada célula disponible en paralelo
//   - La primera que devuelva datos "gana" (las demás son ignoradas)
//   - Si ninguna tiene el dato, retornamos nil
//
// Nota: Esto tiene mayor costo que el enrutamiento determinista, pero
// garantiza compatibilidad con peticiones sin ID (APIs legacy).
func (r *Router) BroadcastGet(id string) (*ResultadoBroadcast, error) {
	r.mu.RLock()
	celulasActivas := r.obtenerCelulasActivasNoLocked()
	r.mu.RUnlock()

	if len(celulasActivas) == 0 {
		return nil, fmt.Errorf("no hay células disponibles para broadcast")
	}

	// Canal que recibe el primer resultado exitoso.
	// Usamos buffer de 1 para que la goroutine ganadora no bloquee.
	resultado := make(chan *ResultadoBroadcast, 1)

	// WaitGroup para saber cuándo terminaron TODAS las goroutines.
	var wg sync.WaitGroup

	// Lanzamos una goroutine por cada célula activa (scatter)
	for _, c := range celulasActivas {
		wg.Add(1)
		// Importante: pasamos 'c' como parámetro (no lo capturamos en el closure)
		// para evitar el bug clásico de closures en Go con variables de loop.
		go func(celula *cell.Celula) {
			defer wg.Done()

			reg, err := celula.Obtener(id)
			if err != nil || reg == nil {
				// Esta célula no tiene el dato, no hacemos nada
				return
			}

			// ¡Encontramos el dato! Intentamos enviarlo al canal.
			// select non-blocking: si el canal ya tiene un resultado (otro ganó antes),
			// simplemente ignoramos este resultado.
			select {
			case resultado <- &ResultadoBroadcast{Celula: celula.ID, Registro: reg}:
				// Enviado exitosamente
			default:
				// El canal ya tiene un resultado, descartamos este (gather)
			}
		}(c)
	}

	// Esperamos a que terminen todas las goroutines en background
	// para no dejar goroutines colgadas (goroutine leak)
	go func() {
		wg.Wait()
		close(resultado)
	}()

	// Recibimos el primer resultado que llegue (o nil si el canal se cierra vacío)
	res, ok := <-resultado
	if !ok || res == nil {
		return nil, nil // Ninguna célula tenía el dato
	}

	return res, nil
}

// Rebalancear redistribuye los rangos de IDs entre células.
// Este es el método que el agente de IA invoca via MCP cuando detecta
// que una célula está colapsada y necesita redirigir su tráfico.
//
// Parámetros:
//   - nuevosRangos: mapa con los nuevos rangos para cada célula.
//     Ejemplo: {"A": {0, 1499999}, "B": {1500000, 1999999}} (Célula C colapsó,
//     su tráfico se reparte entre A y B)
//
// Este método es thread-safe y no detiene el sistema (zero-downtime).
// Las peticiones en vuelo terminan con los rangos viejos;
// las nuevas peticiones ya usan los rangos nuevos.
//
// Importante: sincroniza tanto router.rangos como Celula.RangoMin/RangoMax
// para que /api/metrics y /api/cells reflejen los mismos valores.
func (r *Router) Rebalancear(nuevosRangos map[string]RangoID) error {
	// Validamos que todos los IDs de células existan en el sistema
	for idCelula := range nuevosRangos {
		if _, existe := r.celulas[idCelula]; !existe {
			return fmt.Errorf("célula '%s' no existe en el sistema", idCelula)
		}
	}

	// Lock de escritura: bloqueamos enrutamiento mientras actualizamos rangos.
	// El tiempo de bloqueo es mínimo (solo una asignación de mapa).
	r.mu.Lock()
	defer r.mu.Unlock()

	// Actualizamos los rangos atómicamente en el router y en cada célula
	for idCelula, rango := range nuevosRangos {
		r.rangos[idCelula] = rango
		if celula, ok := r.celulas[idCelula]; ok {
			celula.RangoMin = rango.Min
			celula.RangoMax = rango.Max
		}
	}

	return nil
}

// ObtenerRangos devuelve el mapa actual de rangos de IDs.
// Útil para que el agente de IA conozca la configuración actual antes de rebalancear.
func (r *Router) ObtenerRangos() map[string]RangoID {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Hacemos una copia para no exponer el mapa interno
	copia := make(map[string]RangoID, len(r.rangos))
	for k, v := range r.rangos {
		copia[k] = v
	}
	return copia
}

// ObtenerCelulas devuelve todas las células registradas en el router.
func (r *Router) ObtenerCelulas() map[string]*cell.Celula {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.celulas
}

// ObtenerCelula devuelve una célula específica por su ID.
func (r *Router) ObtenerCelula(id string) (*cell.Celula, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, existe := r.celulas[id]
	return c, existe
}

// obtenerCelulasActivasNoLocked devuelve las células que pueden procesar peticiones.
// PRECONDICIÓN: El caller debe tener el lock adquirido.
func (r *Router) obtenerCelulasActivasNoLocked() []*cell.Celula {
	activas := make([]*cell.Celula, 0, len(r.celulas))
	for _, idCelula := range r.ordenCelulas {
		c := r.celulas[idCelula]
		if c.EstaDisponible() {
			activas = append(activas, c)
		}
	}
	return activas
}

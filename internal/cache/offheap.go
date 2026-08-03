// Package cache implementa un caché off-heap que evita el Garbage Collector de Go.
//
// --- ¿Por qué necesitamos esto? ---
// En Go, el Recolector de Basura (GC) escanea toda la memoria del heap en busca de
// referencias (punteros) para saber qué puede liberar. Si tenemos un mapa nativo de
// Go con millones de entradas que contienen strings o punteros, el GC necesita
// recorrer CADA entrada en CADA ciclo de limpieza.
//
// Esto fue medido en Mercado Libre: el GC llegó a consumir el 45% de la CPU en
// servicios de búsqueda con cachés grandes.
//
// --- La solución: BigCache ---
// BigCache almacena los datos en bloques de bytes "fuera del montículo" (off-heap).
// Al no haber punteros dentro de la estructura, el GC simplemente la ignora por completo.
// El costo: tenemos que serializar/deserializar los datos a/desde []byte.
// El beneficio: latencias < 100 microsegundos y eliminación de pausas de GC.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/allegro/bigcache/v3"
)

// CacheOffHeap es un caché de alta velocidad que opera fuera del montículo de Go.
// Está diseñado para almacenar tablas de enrutamiento y datos de sesión
// que se leen millones de veces por minuto.
type CacheOffHeap struct {
	// bc es la instancia de BigCache. Internamente maneja shards (particiones)
	// para reducir la contención de locks en acceso concurrente.
	bc *bigcache.BigCache

	// Estadísticas propias que BigCache no expone directamente
	hits   int64
	misses int64
}

// Estadisticas contiene métricas del rendimiento del caché.
type Estadisticas struct {
	// TasaHit indica qué porcentaje de lecturas encuentran el dato en caché.
	// Un valor alto (>90%) significa que el caché está siendo efectivo.
	TasaHit float64 `json:"tasa_hit"`

	// CantidadEntradas es el número de elementos almacenados actualmente.
	CantidadEntradas int `json:"cantidad_entradas"`

	// CapacidadUsadaBytes es cuántos bytes ocupa el caché en RAM.
	CapacidadUsadaBytes int64 `json:"capacidad_usada_bytes"`
}

// NuevoCacheOffHeap inicializa el caché off-heap.
//
// Parámetros:
//   - ttl: Tiempo de vida de cada entrada. Después de este tiempo, la entrada expira
//     automáticamente. Útil para datos de routing que cambian frecuentemente.
//   - capacidadMB: Tamaño máximo del caché en megabytes. Cuando se llena,
//     las entradas más viejas son eliminadas (política eviction FIFO).
func NuevoCacheOffHeap(ttl time.Duration, capacidadMB int) (*CacheOffHeap, error) {
	// Configuramos BigCache con opciones optimizadas para alta concurrencia
	config := bigcache.Config{
		// Shards divide el caché en N particiones independientes.
		// Cada shard tiene su propio lock, así 1024 goroutines pueden escribir
		// en paralelo sin bloquearse entre sí.
		// Debe ser potencia de 2.
		Shards: 1024,

		// LifeWindow es el TTL (tiempo de vida) de las entradas.
		LifeWindow: ttl,

		// CleanWindow es cuán seguido el GC interno de BigCache limpia entradas expiradas.
		// 5 minutos es un buen balance entre precisión y rendimiento.
		CleanWindow: 5 * time.Minute,

		// MaxEntriesInWindow estima cuántas entradas esperamos recibir por LifeWindow.
		// Ayuda a BigCache a pre-alocar memoria eficientemente.
		MaxEntriesInWindow: 1000 * 10 * 60,

		// MaxEntrySize es el tamaño máximo de una entrada individual en bytes.
		MaxEntrySize: 500,

		// HardMaxCacheSize es el límite absoluto de RAM en MB.
		// Cuando se alcanza, las entradas más viejas son descartadas.
		HardMaxCacheSize: capacidadMB,

		// Verbose false: no imprimimos logs internos de BigCache
		Verbose: false,
	}

	// Creamos la instancia de BigCache
	bc, err := bigcache.New(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("error al inicializar BigCache: %w", err)
	}

	return &CacheOffHeap{bc: bc}, nil
}

// Guardar serializa el valor a JSON y lo almacena en el caché off-heap.
//
// La serialización a []byte es el precio que pagamos por evadir el GC.
// En producción, se usaría un codec más rápido (MessagePack, FlatBuffers),
// pero JSON es suficiente para demostrar el concepto.
func (c *CacheOffHeap) Guardar(clave string, valor interface{}) error {
	// Serializamos el valor a bytes. BigCache almacena []byte, no interfaces.
	datos, err := json.Marshal(valor)
	if err != nil {
		return fmt.Errorf("error al serializar valor para clave '%s': %w", clave, err)
	}

	// Set es O(1) en BigCache gracias a los shards y la estructura ring-buffer interna
	return c.bc.Set(clave, datos)
}

// Obtener recupera un valor del caché y lo deserializa al tipo destino.
//
// Parámetros:
//   - clave: La clave de búsqueda
//   - destino: Puntero al struct donde se va a deserializar el valor (ej: &miStruct)
//
// Retorna (false, nil) si la clave no existe (miss). Retorna (true, nil) si encontró el dato.
func (c *CacheOffHeap) Obtener(clave string, destino interface{}) (bool, error) {
	datos, err := c.bc.Get(clave)
	if err != nil {
		// ErrEntryNotFound significa que la clave no existe (miss normal, no un error real)
		if err == bigcache.ErrEntryNotFound {
			c.misses++
			return false, nil
		}
		// Cualquier otro error sí es un problema
		return false, fmt.Errorf("error al leer clave '%s' del caché: %w", clave, err)
	}

	// Cache hit: tenemos los datos, los deserializamos al tipo destino
	c.hits++
	if err := json.Unmarshal(datos, destino); err != nil {
		return false, fmt.Errorf("error al deserializar valor para clave '%s': %w", clave, err)
	}

	return true, nil
}

// Eliminar remueve una entrada del caché.
// Útil cuando sabemos que un dato cambió y queremos forzar una recarga.
func (c *CacheOffHeap) Eliminar(clave string) error {
	return c.bc.Delete(clave)
}

// ObtenerEstadisticas devuelve métricas de rendimiento del caché.
// La "tasa de hit" es la métrica más importante: indica qué tan efectivo es el caché.
func (c *CacheOffHeap) ObtenerEstadisticas() Estadisticas {
	total := c.hits + c.misses
	tasaHit := 0.0
	if total > 0 {
		// Calculamos el porcentaje de peticiones que encontraron el dato en caché
		tasaHit = float64(c.hits) / float64(total) * 100
	}

	return Estadisticas{
		TasaHit:             tasaHit,
		CantidadEntradas:    c.bc.Len(),
		CapacidadUsadaBytes: int64(c.bc.Capacity()),
	}
}

// Cerrar libera los recursos del caché. Debe llamarse al cerrar la aplicación.
func (c *CacheOffHeap) Cerrar() error {
	return c.bc.Close()
}

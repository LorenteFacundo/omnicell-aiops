/**
 * gateway-client.ts
 *
 * Cliente HTTP que el MCP Server usa para hablar con el Gateway en Go.
 * Centraliza todas las llamadas HTTP en un solo lugar para no repetir
 * lógica de URL y manejo de errores en cada tool.
 */
/**
 * EstadoCelula describe el estado de salud y métricas de una célula individual.
 * Estos son los datos que el agente de IA analiza para detectar anomalías.
 */
export interface EstadoCelula {
    id: string;
    estado: "healthy" | "degraded" | "collapsed";
    rango_min: number;
    rango_max: number;
    latencia_p99_ms: number;
    latencia_promedio_ms: number;
    total_peticiones: number;
    peticiones_fallidas: number;
    cantidad_registros: number;
    alerta: boolean;
    mensaje_alerta?: string;
}
/**
 * EstadoGC describe las métricas del Garbage Collector de Go.
 * Con BigCache activo, ultima_pausa_ms debe ser casi 0.
 */
export interface EstadoGC {
    num_ciclos: number;
    ultima_pausa_ms: number;
    pausa_total_ms: number;
    mem_heap_mb: number;
    allocaciones_totales: number;
}
/**
 * SnapshotSistema es el estado completo del sistema en un momento dado.
 * Incluye métricas de todas las células, el GC y el caché.
 */
export interface SnapshotSistema {
    timestamp: string;
    requests_por_segundo: number;
    total_requests: number;
    requests_exitosos: number;
    requests_fallidos: number;
    celulas: EstadoCelula[];
    gc: EstadoGC;
    cache: {
        tasa_hit_porcentaje: number;
        cantidad_entradas: number;
        capacidad_usada_mb: number;
    };
    hay_celulas_colapsadas: boolean;
}
/**
 * RangoID define el rango de IDs numéricos de una célula.
 */
export interface RangoID {
    min: number;
    max: number;
}
/**
 * GatewayClient es el cliente HTTP que comunica el MCP Server con el Gateway Go.
 * Usa axios para HTTP con timeout configurado para no bloquear el agente de IA.
 */
export declare class GatewayClient {
    private readonly http;
    constructor(baseUrl?: string);
    /**
     * obtenerMetricas llama a GET /api/metrics y devuelve el snapshot completo.
     * Este es el método más importante: la IA lo usa para "ver" el sistema.
     */
    obtenerMetricas(): Promise<SnapshotSistema>;
    /**
     * rebalancearRangos llama a POST /api/rebalance con los nuevos rangos.
     * La IA usa esto para redirigir tráfico de una célula colapsada a las sanas.
     */
    rebalancearRangos(nuevosRangos: Record<string, RangoID>): Promise<{
        mensaje: string;
        datos: Record<string, RangoID>;
    }>;
    /**
     * colapsarCelula llama a POST /api/cells/{id}/collapse.
     * Simula un fallo catastrófico de base de datos (Chaos Engineering).
     */
    colapsarCelula(cellId: string): Promise<{
        mensaje: string;
    }>;
    /**
     * degradarCelula pone la célula en estado degradado (alta latencia).
     */
    degradarCelula(cellId: string): Promise<{
        mensaje: string;
    }>;
    /**
     * recuperarCelula vuelve una célula colapsada al estado saludable.
     */
    recuperarCelula(cellId: string): Promise<{
        mensaje: string;
    }>;
    /**
     * obtenerCelulas devuelve el estado actual de todas las células.
     */
    obtenerCelulas(): Promise<EstadoCelula[]>;
    /**
     * inyectarCarga lanza peticiones artificiales al sistema.
     * Útil para estresar el sistema antes del demo.
     */
    inyectarCarga(cantidad: number, celulaObjetivo?: string, paralelo?: boolean): Promise<{
        mensaje: string;
    }>;
}
export declare const gatewayClient: GatewayClient;
//# sourceMappingURL=gateway-client.d.ts.map
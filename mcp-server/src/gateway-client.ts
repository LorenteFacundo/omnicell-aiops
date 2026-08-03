/**
 * gateway-client.ts
 *
 * Cliente HTTP que el MCP Server usa para hablar con el Gateway en Go.
 * Centraliza todas las llamadas HTTP en un solo lugar para no repetir
 * lógica de URL y manejo de errores en cada tool.
 */

import axios, { AxiosInstance } from "axios";

// URL del Gateway Go. Por defecto apunta al desarrollo local.
// Para producción, se puede cambiar via variable de entorno GATEWAY_URL.
const GATEWAY_URL = process.env.GATEWAY_URL ?? "http://localhost:8080";

// ---- Tipos que mapean las respuestas del Gateway ----

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
export class GatewayClient {
  private readonly http: AxiosInstance;

  constructor(baseUrl: string = GATEWAY_URL) {
    this.http = axios.create({
      baseURL: baseUrl,
      // Timeout de 10 segundos: si el Gateway no responde en ese tiempo,
      // el agente de IA recibe un error claro en lugar de esperar indefinidamente.
      timeout: 10_000,
      headers: { "Content-Type": "application/json" },
    });
  }

  /**
   * obtenerMetricas llama a GET /api/metrics y devuelve el snapshot completo.
   * Este es el método más importante: la IA lo usa para "ver" el sistema.
   */
  async obtenerMetricas(): Promise<SnapshotSistema> {
    const respuesta = await this.http.get<SnapshotSistema>("/api/metrics");
    return respuesta.data;
  }

  /**
   * rebalancearRangos llama a POST /api/rebalance con los nuevos rangos.
   * La IA usa esto para redirigir tráfico de una célula colapsada a las sanas.
   */
  async rebalancearRangos(
    nuevosRangos: Record<string, RangoID>
  ): Promise<{ mensaje: string; datos: Record<string, RangoID> }> {
    const respuesta = await this.http.post("/api/rebalance", {
      nuevos_rangos: nuevosRangos,
    });
    return respuesta.data;
  }

  /**
   * colapsarCelula llama a POST /api/cells/{id}/collapse.
   * Simula un fallo catastrófico de base de datos (Chaos Engineering).
   */
  async colapsarCelula(cellId: string): Promise<{ mensaje: string }> {
    const respuesta = await this.http.post(`/api/cells/${cellId}/collapse`);
    return respuesta.data;
  }

  /**
   * degradarCelula pone la célula en estado degradado (alta latencia).
   */
  async degradarCelula(cellId: string): Promise<{ mensaje: string }> {
    const respuesta = await this.http.post(`/api/cells/${cellId}/degrade`);
    return respuesta.data;
  }

  /**
   * recuperarCelula vuelve una célula colapsada al estado saludable.
   */
  async recuperarCelula(cellId: string): Promise<{ mensaje: string }> {
    const respuesta = await this.http.post(`/api/cells/${cellId}/recover`);
    return respuesta.data;
  }

  /**
   * obtenerCelulas devuelve el estado actual de todas las células.
   */
  async obtenerCelulas(): Promise<EstadoCelula[]> {
    const respuesta = await this.http.get<{ datos: EstadoCelula[] }>(
      "/api/cells"
    );
    return respuesta.data.datos;
  }

  /**
   * inyectarCarga lanza peticiones artificiales al sistema.
   * Útil para estresar el sistema antes del demo.
   */
  async inyectarCarga(
    cantidad: number,
    celulaObjetivo?: string,
    paralelo: boolean = true
  ): Promise<{ mensaje: string }> {
    const respuesta = await this.http.post("/api/load-test", {
      cantidad_requests: cantidad,
      celula_objetivo: celulaObjetivo ?? "",
      paralelo,
    });
    return respuesta.data;
  }
}

// Instancia singleton del cliente (compartida entre todas las tools)
export const gatewayClient = new GatewayClient();

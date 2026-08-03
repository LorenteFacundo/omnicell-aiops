"use strict";
/**
 * gateway-client.ts
 *
 * Cliente HTTP que el MCP Server usa para hablar con el Gateway en Go.
 * Centraliza todas las llamadas HTTP en un solo lugar para no repetir
 * lógica de URL y manejo de errores en cada tool.
 */
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.gatewayClient = exports.GatewayClient = void 0;
const axios_1 = __importDefault(require("axios"));
// URL del Gateway Go. Por defecto apunta al desarrollo local.
// Para producción, se puede cambiar via variable de entorno GATEWAY_URL.
const GATEWAY_URL = process.env.GATEWAY_URL ?? "http://localhost:8080";
/**
 * GatewayClient es el cliente HTTP que comunica el MCP Server con el Gateway Go.
 * Usa axios para HTTP con timeout configurado para no bloquear el agente de IA.
 */
class GatewayClient {
    http;
    constructor(baseUrl = GATEWAY_URL) {
        this.http = axios_1.default.create({
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
    async obtenerMetricas() {
        const respuesta = await this.http.get("/api/metrics");
        return respuesta.data;
    }
    /**
     * rebalancearRangos llama a POST /api/rebalance con los nuevos rangos.
     * La IA usa esto para redirigir tráfico de una célula colapsada a las sanas.
     */
    async rebalancearRangos(nuevosRangos) {
        const respuesta = await this.http.post("/api/rebalance", {
            nuevos_rangos: nuevosRangos,
        });
        return respuesta.data;
    }
    /**
     * colapsarCelula llama a POST /api/cells/{id}/collapse.
     * Simula un fallo catastrófico de base de datos (Chaos Engineering).
     */
    async colapsarCelula(cellId) {
        const respuesta = await this.http.post(`/api/cells/${cellId}/collapse`);
        return respuesta.data;
    }
    /**
     * degradarCelula pone la célula en estado degradado (alta latencia).
     */
    async degradarCelula(cellId) {
        const respuesta = await this.http.post(`/api/cells/${cellId}/degrade`);
        return respuesta.data;
    }
    /**
     * recuperarCelula vuelve una célula colapsada al estado saludable.
     */
    async recuperarCelula(cellId) {
        const respuesta = await this.http.post(`/api/cells/${cellId}/recover`);
        return respuesta.data;
    }
    /**
     * obtenerCelulas devuelve el estado actual de todas las células.
     */
    async obtenerCelulas() {
        const respuesta = await this.http.get("/api/cells");
        return respuesta.data.datos;
    }
    /**
     * inyectarCarga lanza peticiones artificiales al sistema.
     * Útil para estresar el sistema antes del demo.
     */
    async inyectarCarga(cantidad, celulaObjetivo, paralelo = true) {
        const respuesta = await this.http.post("/api/load-test", {
            cantidad_requests: cantidad,
            celula_objetivo: celulaObjetivo ?? "",
            paralelo,
        });
        return respuesta.data;
    }
}
exports.GatewayClient = GatewayClient;
// Instancia singleton del cliente (compartida entre todas las tools)
exports.gatewayClient = new GatewayClient();
//# sourceMappingURL=gateway-client.js.map
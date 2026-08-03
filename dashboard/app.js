/**
 * app.js — Lógica del Dashboard OmniCell
 *
 * Este archivo maneja:
 *   1. Polling al Gateway cada 1 segundo para obtener métricas
 *   2. Actualización del estado visual de cada célula
 *   3. Gráfico de latencia p99 en tiempo real (Chart.js)
 *   4. Log de eventos del sistema
 *   5. Botones de control (colapsar, degradar, recuperar, load test)
 *
 * El Dashboard se comunica DIRECTAMENTE con el Gateway Go (no con el MCP Server).
 * El MCP Server es para los agentes de IA. El Dashboard es para el operador humano.
 */

// ========== Configuración ==========
const GATEWAY_URL    = 'http://localhost:8080';  // URL del Gateway Go
const POLL_INTERVAL  = 1000;                      // Polling cada 1 segundo
const MAX_LOG_ENTRIES = 100;                      // Máximo de entradas en el log
const CHART_WINDOW   = 30;                        // Últimas 30 lecturas en el gráfico

// ========== Estado global ==========
// Guardamos el último snapshot para comparar y detectar cambios de estado
let estadoAnterior = {};
let contadorRefresh = 0;

// ========== Inicialización del gráfico de latencia ==========
const ctx = document.getElementById('latency-chart').getContext('2d');

// Preparamos los datos históricos para el gráfico
// Cada célula tiene su propia línea con un color distintivo
const datosGrafico = {
  labels: Array(CHART_WINDOW).fill(''),
  datasets: [
    {
      label: 'Célula A (p99)',
      data: Array(CHART_WINDOW).fill(null),
      borderColor: '#10b981',
      backgroundColor: 'rgba(16, 185, 129, 0.08)',
      borderWidth: 2,
      pointRadius: 0,
      fill: true,
      tension: 0.4,  // Curva suave
    },
    {
      label: 'Célula B (p99)',
      data: Array(CHART_WINDOW).fill(null),
      borderColor: '#06b6d4',
      backgroundColor: 'rgba(6, 182, 212, 0.08)',
      borderWidth: 2,
      pointRadius: 0,
      fill: true,
      tension: 0.4,
    },
    {
      label: 'Célula C (p99)',
      data: Array(CHART_WINDOW).fill(null),
      borderColor: '#8b5cf6',
      backgroundColor: 'rgba(139, 92, 246, 0.08)',
      borderWidth: 2,
      pointRadius: 0,
      fill: true,
      tension: 0.4,
    },
  ],
};

const chart = new Chart(ctx, {
  type: 'line',
  data: datosGrafico,
  options: {
    responsive: true,
    maintainAspectRatio: false,
    animation: { duration: 200 },  // Animación corta para que la actualización se sienta viva
    interaction: { intersect: false, mode: 'index' },
    plugins: {
      legend: {
        labels: {
          color: '#94a3b8',
          font: { family: "'JetBrains Mono', monospace", size: 11 },
          boxWidth: 12,
        },
      },
      tooltip: {
        backgroundColor: '#111827',
        borderColor: '#1e2d45',
        borderWidth: 1,
        titleColor: '#f1f5f9',
        bodyColor: '#94a3b8',
        callbacks: {
          // Formateamos el tooltip para mostrar "X.XXX ms"
          label: (ctx) => ` ${ctx.dataset.label}: ${ctx.parsed.y?.toFixed(3) ?? '—'} ms`,
        },
      },
    },
    scales: {
      x: {
        display: false,  // Ocultamos el eje X (son timestamps sin texto útil)
      },
      y: {
        position: 'right',
        beginAtZero: true,
        grid: {
          color: 'rgba(255,255,255,0.04)',
        },
        ticks: {
          color: '#475569',
          font: { family: "'JetBrains Mono', monospace", size: 10 },
          callback: (val) => `${val} ms`,
        },
      },
    },
  },
});

// ========== Polling principal ==========

/**
 * fetchMetrics llama al Gateway y actualiza toda la UI.
 * Se ejecuta cada POLL_INTERVAL milisegundos.
 */
async function fetchMetrics() {
  try {
    const respuesta = await fetch(`${GATEWAY_URL}/api/metrics`);

    if (!respuesta.ok) {
      throw new Error(`Gateway respondió ${respuesta.status}`);
    }

    const snapshot = await respuesta.json();

    // Actualizamos todos los elementos de la UI con el nuevo snapshot
    actualizarKPIs(snapshot);
    actualizarCelulas(snapshot.celulas);
    actualizarGrafico(snapshot.celulas);
    actualizarGC(snapshot.gc, snapshot.cache);
    actualizarEstadoGeneral(snapshot);

    // Contador de refreshes exitosos
    contadorRefresh++;
    document.getElementById('refresh-counter').textContent =
      `#${contadorRefresh} · ${new Date().toLocaleTimeString()}`;

  } catch (error) {
    // Si el Gateway no responde, lo mostramos en el estado
    document.getElementById('system-status-badge').className = 'status-badge critical';
    document.getElementById('system-status-text').textContent = 'GATEWAY OFFLINE';
    agregarLog('error', `Gateway no disponible: ${error.message}. ¿Está corriendo en :8080?`);
  }
}

// ========== Actualizadores de UI ==========

/**
 * actualizarKPIs actualiza los 4 números grandes de la fila superior.
 */
function actualizarKPIs(snapshot) {
  // Requests por segundo
  setText('kpi-rps', snapshot.requests_por_segundo.toFixed(1));

  // Total de requests (con separador de miles)
  setText('kpi-total', snapshot.total_requests.toLocaleString());

  // Tasa de error
  const tasaError = snapshot.total_requests > 0
    ? (snapshot.requests_fallidos / snapshot.total_requests * 100).toFixed(2)
    : '0.00';

  const errorEl = document.getElementById('kpi-error-rate');
  errorEl.textContent = `${tasaError}%`;
  // Color condicional: rojo si hay errores, blanco si no
  errorEl.className = 'kpi-value ' + (parseFloat(tasaError) > 1 ? 'red' : '');

  // Cache hit rate
  setText('kpi-cache-hit', snapshot.cache.tasa_hit_porcentaje.toFixed(1) + '%');
}

/**
 * actualizarCelulas actualiza las 3 cards de células.
 * Si el estado de una célula cambió, agrega un log del evento.
 */
function actualizarCelulas(celulas) {
  for (const celula of celulas) {
    const id = celula.id;
    const card = document.getElementById(`cell-${id}`);
    if (!card) continue;

    // Detectamos cambio de estado para loguear el evento
    const estadoPrevio = estadoAnterior[id]?.estado;
    if (estadoPrevio && estadoPrevio !== celula.estado) {
      const mensajes = {
        healthy:   `Célula ${id} volvió a estado HEALTHY ✅`,
        degraded:  `Célula ${id} entró en estado DEGRADED ⚠️ — latencia elevada`,
        collapsed: `Célula ${id} COLAPSADA 💥 — compartimento hermético activado`,
      };
      const tipos = { healthy: 'success', degraded: 'warn', collapsed: 'error' };
      agregarLog(tipos[celula.estado], mensajes[celula.estado]);
    }

    // Actualizamos la clase CSS de la card para cambiar los colores
    card.className = `cell-card ${celula.estado}`;

    // Badge de estado
    setText(`cell-${id}-badge`, celula.estado);

    // Latencia p99
    setText(`cell-${id}-p99`, celula.latencia_p99_ms.toFixed(2));

    // Total requests
    setText(`cell-${id}-reqs`, celula.total_peticiones.toLocaleString());

    // Contador de fallidas
    setText(`cell-${id}-fail`, `${celula.peticiones_fallidas} fallidas`);

    // Barra de carga: mostramos el % de requests fallidas como indicador de estrés
    // Si la célula está colapsada, 100% rojo
    let porcentajeCarga = 0;
    if (celula.estado === 'collapsed') {
      porcentajeCarga = 100;
    } else if (celula.total_peticiones > 0) {
      // Usamos latencia como proxy de carga (mayor latencia = más carga)
      // Normalizamos a un rango 0-100 asumiendo que 100ms p99 es "full load"
      porcentajeCarga = Math.min(celula.latencia_p99_ms / 100 * 100, 100);
    }

    document.getElementById(`cell-${id}-load`).style.width = `${porcentajeCarga}%`;

    // Guardamos el estado para comparar en el próximo tick
    estadoAnterior[id] = celula;
  }
}

/**
 * actualizarGrafico agrega los nuevos valores de latencia p99 al gráfico.
 * El gráfico tiene una ventana deslizante de CHART_WINDOW lecturas.
 */
function actualizarGrafico(celulas) {
  const timestamp = new Date().toLocaleTimeString('es', { timeStyle: 'medium' });

  // Rotamos el buffer de labels (ventana deslizante)
  datosGrafico.labels.push(timestamp);
  datosGrafico.labels.shift();

  // Mapeamos cada célula a su dataset en el gráfico (por orden: A, B, C)
  const ordenCelulas = ['A', 'B', 'C'];

  ordenCelulas.forEach((id, index) => {
    const celula = celulas.find(c => c.id === id);
    const dataset = datosGrafico.datasets[index];

    // Si la célula está colapsada, graficamos null (brecha en la línea)
    // Esto hace visualmente obvio cuándo colapsó
    const valor = celula?.estado === 'collapsed' ? null : (celula?.latencia_p99_ms ?? null);

    dataset.data.push(valor);
    dataset.data.shift();
  });

  // Actualizamos el color de la línea de Célula C según su estado
  const celulaC = celulas.find(c => c.id === 'C');
  if (celulaC) {
    const colorC = celulaC.estado === 'collapsed' ? '#ef4444'
                 : celulaC.estado === 'degraded'  ? '#f59e0b'
                 : '#8b5cf6';
    datosGrafico.datasets[2].borderColor = colorC;
  }

  // Notificamos a Chart.js que los datos cambiaron (sin re-render completo)
  chart.update('none'); // 'none' = sin animación (más suave para polling rápido)
}

/**
 * actualizarGC actualiza el panel de Garbage Collector.
 */
function actualizarGC(gc, cache) {
  setText('gc-cycles',     gc.num_ciclos);
  setText('gc-last-pause', `${gc.ultima_pausa_ms.toFixed(3)} ms`);
  setText('gc-heap',       `${gc.mem_heap_mb.toFixed(1)} MB`);
  setText('gc-cache-hit',  `${cache.tasa_hit_porcentaje.toFixed(1)}%`);
}

/**
 * actualizarEstadoGeneral actualiza el badge de estado en el header.
 */
function actualizarEstadoGeneral(snapshot) {
  const badge = document.getElementById('system-status-badge');
  const texto = document.getElementById('system-status-text');
  const dot   = document.getElementById('system-status-dot');

  if (snapshot.hay_celulas_colapsadas) {
    badge.className = 'status-badge critical';
    texto.textContent = 'CRÍTICO';
    dot.className = 'status-dot';
  } else {
    // Revisamos si alguna célula está degradada
    const hayDegradadas = snapshot.celulas.some(c => c.estado === 'degraded');
    if (hayDegradadas) {
      badge.className = 'status-badge degraded';
      texto.textContent = 'DEGRADADO';
      dot.className = 'status-dot pulse';
    } else {
      badge.className = 'status-badge operational';
      texto.textContent = 'OPERACIONAL';
      dot.className = 'status-dot pulse';
    }
  }
}

// ========== Controles de células (Chaos Engineering) ==========

/**
 * collapseCell colapsa una célula vía el Gateway.
 * Equivalente a la tool MCP "simulate_bulkhead_collapse" con tipo="collapse".
 */
window.collapseCell = async function(cellId) {
  try {
    const res = await fetch(`${GATEWAY_URL}/api/cells/${cellId}/collapse`, { method: 'POST' });
    const data = await res.json();
    agregarLog('error', `💥 Colapso manual: ${data.mensaje ?? data.error}`);
  } catch (e) {
    agregarLog('error', `Error al colapsar célula ${cellId}: ${e.message}`);
  }
};

/**
 * degradeCell degrada una célula (alta latencia).
 */
window.degradeCell = async function(cellId) {
  try {
    const res = await fetch(`${GATEWAY_URL}/api/cells/${cellId}/degrade`, { method: 'POST' });
    const data = await res.json();
    agregarLog('warn', `⚡ Degradación manual: ${data.mensaje ?? data.error}`);
  } catch (e) {
    agregarLog('error', `Error al degradar célula ${cellId}: ${e.message}`);
  }
};

/**
 * recoverCell recupera una célula colapsada o degradada.
 */
window.recoverCell = async function(cellId) {
  try {
    const res = await fetch(`${GATEWAY_URL}/api/cells/${cellId}/recover`, { method: 'POST' });
    const data = await res.json();
    agregarLog('success', `✅ Recuperación: ${data.mensaje ?? data.error}`);
  } catch (e) {
    agregarLog('error', `Error al recuperar célula ${cellId}: ${e.message}`);
  }
};

// ========== Controles de Load Test ==========

/**
 * loadTest inyecta tráfico artificial al sistema.
 * Útil para estresar una célula y provocar el colapso para el demo.
 */
window.loadTest = async function(cantidad, celulaObjetivo = '', paralelo = true) {
  agregarLog('info', `🚀 Inyectando ${cantidad.toLocaleString()} requests${celulaObjetivo ? ` → Célula ${celulaObjetivo}` : ' distribuidos'}...`);
  try {
    const res = await fetch(`${GATEWAY_URL}/api/load-test`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        cantidad_requests: cantidad,
        celula_objetivo: celulaObjetivo,
        paralelo,
      }),
    });
    const data = await res.json();
    agregarLog('success', `✅ ${data.mensaje}`);
  } catch (e) {
    agregarLog('error', `Error en load test: ${e.message}`);
  }
};

/**
 * rebalanceToAB redistribuye los rangos de C hacia A y B.
 * Esto es lo que el agente de IA hace cuando ejecuta "rebalance_id_ranges".
 */
window.rebalanceToAB = async function() {
  agregarLog('info', '⚡ Ejecutando rebalanceo: tráfico de C → A y B...');
  try {
    const res = await fetch(`${GATEWAY_URL}/api/rebalance`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        nuevos_rangos: {
          // Rango A se expande: absorbe la mitad del rango C
          A: { min: 0, max: 1_499_999 },
          // Rango B absorbe la otra mitad del rango C
          B: { min: 1_500_000, max: 2_999_999 },
          // Rango C queda vacío (ningún ID nuevo irá a C)
          C: { min: 3_000_000, max: 3_000_000 },
        },
      }),
    });
    const data = await res.json();
    agregarLog('success', `✅ Rebalanceo exitoso: ${data.mensaje}`);
  } catch (e) {
    agregarLog('error', `Error al rebalancear: ${e.message}`);
  }
};

/**
 * runFullDemo ejecuta la secuencia completa del demo de forma automática:
 * 1. Inyecta carga normal
 * 2. Satura la Célula C
 * 3. Colapsa la Célula C
 * 4. Ejecuta el rebalanceo
 *
 * Este es el script que se graba para LinkedIn/GitHub.
 */
window.runFullDemo = async function() {
  agregarLog('info', '🎬 === INICIANDO DEMO COMPLETO ===');

  // Paso 1: Tráfico normal
  agregarLog('info', '[1/4] Inyectando tráfico normal distribuido...');
  await loadTest(2000, '', false);
  await esperar(2000);

  // Paso 2: Pico en Célula C
  agregarLog('warn', '[2/4] ⚠️  Pico masivo de tráfico concentrado en Célula C...');
  await loadTest(8000, 'C', true);
  await esperar(1500);

  // Paso 3: Colapso de Célula C
  agregarLog('error', '[3/4] 💥 Célula C COLAPSANDO por saturación...');
  await collapseCell('C');
  await esperar(3000);

  // Paso 4: Rebalanceo automático
  agregarLog('info', '[4/4] 🤖 Agente IA detectó el colapso. Ejecutando rebalanceo...');
  await rebalanceToAB();
  await esperar(1000);

  agregarLog('success', '🎬 === DEMO COMPLETADO === Sistema estabilizado via AIOps ===');
};

// ========== Log de eventos ==========

/**
 * agregarLog agrega una entrada al log de eventos en tiempo real.
 * Los logs más nuevos aparecen al inicio.
 */
function agregarLog(tipo, mensaje) {
  const logBody = document.getElementById('event-log');
  const ahora = new Date().toLocaleTimeString('es', { timeStyle: 'medium' });

  // Creamos el elemento de log
  const entrada = document.createElement('div');
  entrada.className = 'log-entry';
  entrada.innerHTML = `
    <span class="log-time">${ahora}</span>
    <span class="log-type ${tipo}">${tipo.toUpperCase()}</span>
    <span class="log-message">${mensaje}</span>
  `;

  // Insertamos al principio (más reciente arriba)
  logBody.insertBefore(entrada, logBody.firstChild);

  // Limitamos la cantidad de entradas para no llenar el DOM
  while (logBody.children.length > MAX_LOG_ENTRIES) {
    logBody.removeChild(logBody.lastChild);
  }
}

window.clearLog = function() {
  document.getElementById('event-log').innerHTML = '';
};

// ========== Reloj ==========

function actualizarReloj() {
  document.getElementById('clock').textContent =
    new Date().toLocaleTimeString('es', { timeStyle: 'medium' });
}

// ========== Helpers ==========

function setText(id, valor) {
  const el = document.getElementById(id);
  if (el) el.textContent = valor;
}

function esperar(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

// ========== Inicialización ==========

// Log inicial
agregarLog('info', `Dashboard iniciado. Conectando al Gateway en ${GATEWAY_URL}...`);

// Reloj en tiempo real
actualizarReloj();
setInterval(actualizarReloj, 1000);

// Polling al Gateway
fetchMetrics(); // Primera llamada inmediata para no esperar el primer intervalo
setInterval(fetchMetrics, POLL_INTERVAL);

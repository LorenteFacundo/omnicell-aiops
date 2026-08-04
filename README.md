# 💠 OmniCell AIOps & MCP Engine

> **Motor de Infraestructura Distribución Celular con Orquestación Autónoma AIOps y Protocolo MCP (Model Context Protocol)**

[![Go Version](https://img.shields.io/badge/Go-1.21%2B-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Architecture](https://img.shields.io/badge/Architecture-Cell--Based-purple)](https://aws.amazon.com/blogs/architecture/pattern-cell-based-architecture/)
[![AIOps Engine](https://img.shields.io/badge/AIOps-Self--Healing-green)](#-motor-aiops-nativ-guardian-aut%C3%B3nomo)
[![License](https://img.shields.io/badge/License-MIT-blue)](LICENSE)

OmniCell es una plataforma de infraestructura distribuida de alto rendimiento diseñada en **Go**, orientada a resolver dos de los desafíos más complejos del software moderno: la **mitigación del radio de explosión en caídas masivas** y el **mantenimiento operativo autónomo mediante Inteligencia Artificial (Self-Healing)**.

---

## 🚀 Tecnologías Clave y Arquitectura

```
                       ┌─────────────────────────────────────────┐
                       │          OmniCell Web Dashboard         │
                       │    (Real-time Observability & Chat)     │
                       └───────────────────┬─────────────────────┘
                                           │ HTTP / REST API
                                           ▼
                       ┌─────────────────────────────────────────┐
                       │           OmniCell Gateway              │
                       │ ┌─────────────────────────────────────┐ │
                       │ │   Router Celular Determinista       │ │
                       │ └──────────────────┬──────────────────┘ │
                       │                    │                    │
                       │ ┌──────────────────┴──────────────────┐ │
                       │ │  AIOps Self-Healing Guardian Loop   │ │
                       │ └──────────────────┬──────────────────┘ │
                       └────────────────────┼────────────────────┘
                                            │ Function Calling
                ┌───────────────────────────┼───────────────────────────┐
                ▼                           ▼                           ▼
      ┌──────────────────┐        ┌──────────────────┐        ┌──────────────────┐
      │     Célula A     │        │     Célula B     │        │     Célula C     │
      │  (IDs 0-999k)    │        │   (IDs 1M-1.9M)  │        │   (IDs 2M-2.9M)  │
      └──────────────────┘        └──────────────────┘        └──────────────────┘
```

### 1. 🧬 Arquitectura Basada en Células (Cell-Based Architecture)
Inspirada en las mejores prácticas de **AWS** y **Slack**, OmniCell particiona la infraestructura en células aisladas e independientes (Células A, B y C). Cada célula maneja un rango específico de identificadores. 
* **Reducción del Radio de Explosión**: Si una célula colapsa por una falla o pico extremo de carga, **el 66% del sistema sigue funcionando normalmente**.
* **Rebalanceo Dinámico de Rangos**: El router puede reescribir dinámicamente en memoria qué célula atiende a qué usuarios sin reiniciar ningún servicio.

### 2. ⚡ Caché Off-Heap Libre de Garbage Collector (BigCache)
Para lograr latencias sub-milisegundo en peticiones de alta frecuencia, OmniCell implementa una capa de almacenamiento en caché fuera del Heap de Go (*off-heap memory*).
* Elimina completamente las pausas por recolector de basura (GC Pauses) incluso bajo millones de llaves en memoria.

### 3. 🧠 Motor AIOps Nativo (Guardián Autónomo)
OmniCell integra su propio motor agentico de IA en Go capaz de tomar decisiones deterministas en tiempo real mediante **Function Calling**:
* **Loop Continuo de Monitoreo**: Un goroutine vigila la telemetría del sistema cada 5 segundos.
* **Diagnóstico y Reparación Autónoma (Self-Healing)**: Si detecta una célula colapsada, la IA analiza el incidente y ejecuta herramientas internas (`get_system_metrics`, `rebalance_cell_ranges`) para reorientar el tráfico hacia las células sanas en segundos.
* **Soporte Multi-Proveedor LLM**:
  * 🟢 **OpenAI**: GPT-4o, GPT-4o-mini
  * 🟡 **Google Gemini**: Gemini 2.0 Flash, Gemini 1.5 Pro (vía endpoint compatible con OpenAI)
  * ⚡ **Groq Cloud**: Llama 3.1 70B (respuesta ultra-rápida)
  * 🦙 **Ollama**: Llama 3.1, Qwen 2.5 (100% Local / Offline)
  * 🟣 **Anthropic**: Claude 3.5 Sonnet

### 4. 🔌 Protocolo MCP (Model Context Protocol)
Soporte completo para conectarse como servidor **MCP**, permitiendo que asistentes de desarrollo como **Cursor**, **OpenCode** o **Claude Desktop** puedan inspeccionar y administrar la red celular directamente desde el entorno del programador.

---

## 🛠️ Stack Tecnológico

| Capa | Tecnología | Descripción |
| :--- | :--- | :--- |
| **Lenguaje Core** | **Go 1.21+** | Concurrencia nativa, alto rendimiento, canales y goroutines |
| **HTTP Framework** | **Chi Router** | Routing idiomático y rápido |
| **Caché** | **BigCache** | Caché off-heap sin overhead de GC |
| **IA & LLMs** | **Native Function Calling** | Clientes HTTP custom compatibles con protocolo OpenAI / Gemini / Groq / Ollama |
| **Frontend UI** | **HTML5 / CSS3 (Vanilla)** | Dashboard en modo oscuro con diseño Cyberpunk / AIOps |
| **Gráficos UI** | **Chart.js** | Visualización de latencia p99 en tiempo real |
| **Automatización** | **PowerShell Core** | Scripts de Chaos Engineering |

---

## 🎬 Demos e Instrucciones de Uso

### 📦 1. Requisitos Previos
* **Go 1.21** o superior instalado en tu sistema.
* Un navegador web moderno.

### ⚙️ 2. Compilar y Ejecutar

```bash
# Clonar el repositorio
git clone https://github.com/LorenteFacundo/omnicell-aiops.git
cd omnicell-aiops

# Compilar la aplicación
go build -o bin/gateway.exe ./cmd/gateway/

# Ejecutar el servidor Gateway
./bin/gateway.exe
```

El servidor iniciará en **`http://localhost:8080`**.

---

### 🧪 3. Demo 1: Observabilidad y Chaos Engineering en Vivo (Dashboard Web)

1. Abre tu navegador en **`http://localhost:8080`**.
2. **Simular Tráfico**: Haz clic en los botones `📦 1k req`, `🚀 10k req` o `🎯 Saturar C` para ver cómo se mueven las barras de carga y la gráfica de latencia en tiempo real.
3. **Simular Colapso**: Haz clic en el botón `💥 Colapsar` de la **Célula C**. Verás cómo el estado cambia a rojo (`COLLAPSED`) y las métricas registran la caída.

---

### 🤖 4. Demo 2: Agente AIOps Self-Healing (Autónomo)

1. En el Dashboard web (`http://localhost:8080`), haz clic en el botón **`⚙️ IA`** (barra superior).
2. Ingresa tu API Key (OpenAI, Gemini o Groq) o elige **Ollama** para uso local sin clave.
3. Activa el interruptor **`🛡️ Guardián Auto-Healing`** en la esquina superior derecha.
4. Despliega el panel de chat inferior **`🤖 AIOps Guardian`**.
5. Colapsa la **Célula C** haciendo clic en `💥 Colapsar`.
6. **¡Observa la Magia!** En menos de 5 segundos, el Guardián detectará la anomalía sola, ejecutará la herramienta de rebalanceo y te notificará por el chat explicando el diagnóstico y la solución aplicada.

---

### 💥 5. Demo 3: Script de Chaos Engineering (PowerShell)

Para ejecutar una prueba de estrés continua y ver el rebalanceo automático desde la terminal:

```powershell
# Simula fallas aleatorias en las células y ejecuta el rebalanceo automático
.\scripts\chaos_demo.ps1 -AutoRebalance
```

---

## 📁 Estructura del Proyecto

```
OMNICELL/
├── cmd/
│   └── gateway/         # Punto de entrada principal (main.go)
├── internal/
│   ├── ai/              # Motor AIOps (engine, tools, providers OpenAI/Gemini/Groq/Ollama)
│   ├── api/             # Handlers HTTP REST y middlewares CORS
│   ├── cache/           # Integración de caché off-heap (BigCache)
│   ├── cell/            # Lógica de la célula de infraestructura
│   ├── metrics/         # Colector de telemetría y latencias p99
│   └── router/          # Router determinista de IDs a células
├── dashboard/           # Interfaz web en tiempo real (index.html, index.css, app.js, ai.js)
├── scripts/             # Scripts de simulación de caos (chaos_demo.ps1)
├── MCP_SETUP.md         # Guía de integración con Cursor y MCP
└── README.md
```

---

## 📄 Licencia

Este proyecto está bajo la Licencia **MIT**. Consulta el archivo [LICENSE](LICENSE) para más detalles.

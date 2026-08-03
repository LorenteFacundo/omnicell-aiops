// ai.js — Lógica de Interfaz para el Motor de IA (AIOps Guardian)

const API_URL = 'http://localhost:8080/api';

// Elementos del DOM - Modal de Configuración
const modalConfig = document.getElementById('ai-config-modal');
const btnConfig = document.getElementById('btn-config-ai');
const btnCloseConfig = document.getElementById('close-config');
const btnSaveConfig = document.getElementById('btn-save-config');
const selectProvider = document.getElementById('ai-provider');
const inputApiKey = document.getElementById('ai-api-key');
const inputBaseUrl = document.getElementById('ai-base-url');
const selectModel = document.getElementById('ai-model');
const inputCustomModel = document.getElementById('ai-custom-model');

// Elementos del DOM - Toggle Auto-Healing
const toggleAutoHealing = document.getElementById('auto-healing-toggle');

// Elementos del DOM - Panel de Chat
const chatPanel = document.getElementById('ai-chat-panel');
const btnToggleChat = document.getElementById('btn-toggle-chat');
const chatMessages = document.getElementById('chat-messages');
const chatInput = document.getElementById('chat-input');
const btnSendChat = document.getElementById('btn-send-chat');

// Modelos por defecto por proveedor (para llenar el select)
const modelosPorProveedor = {
    "openai": [
        {id: "gpt-4o", nombre: "GPT-4o"},
        {id: "gpt-4o-mini", nombre: "GPT-4o Mini"}
    ],
    "gemini": [
        {id: "gemini-2.5-flash", nombre: "Gemini 2.5 Flash"},
        {id: "gemini-1.5-pro", nombre: "Gemini 1.5 Pro"}
    ],
    "anthropic": [
        {id: "claude-3-5-sonnet-latest", nombre: "Claude 3.5 Sonnet"}
    ],
    "groq": [
        {id: "llama-3.1-70b-versatile", nombre: "Llama 3.1 70B"},
        {id: "llama3-8b-8192", nombre: "Llama 3 8B"}
    ],
    "ollama": [
        {id: "llama3.1", nombre: "Llama 3.1 8B"},
        {id: "qwen2.5:7b", nombre: "Qwen 2.5 7B"}
    ]
};

let configActual = null;
let lastHistoryCount = 0;

// Inicialización
async function initAI() {
    await fetchConfig();
    setupEventListeners();
    
    // Polling del historial de chat cada 3 segundos
    setInterval(pollHistory, 3000);
}

// -----------------------------------------------------------------------------
// EVENT LISTENERS
// -----------------------------------------------------------------------------
function setupEventListeners() {
    // Abrir/Cerrar Modal Config
    btnConfig.addEventListener('click', () => {
        modalConfig.classList.add('active');
        actualizarVisibilidadCampos();
    });
    
    btnCloseConfig.addEventListener('click', () => {
        modalConfig.classList.remove('active');
    });

    // Cambio de proveedor actualiza modelos
    selectProvider.addEventListener('change', () => {
        actualizarVisibilidadCampos();
        llenarSelectModelos(selectProvider.value);
    });

    // Guardar Configuración
    btnSaveConfig.addEventListener('click', saveConfig);

    // Toggle Auto-Healing
    toggleAutoHealing.addEventListener('change', async (e) => {
        if (e.target.checked && (!configActual || !configActual.configurado)) {
            e.preventDefault();
            e.target.checked = false;
            alert("⚠️ Debes configurar el Motor de IA primero antes de activar el Guardián Autónomo.");
            modalConfig.classList.add('active');
            return;
        }
        await setAutoHealing(e.target.checked);
    });

    // Toggle Panel de Chat
    btnToggleChat.addEventListener('click', () => {
        chatPanel.classList.toggle('collapsed');
        btnToggleChat.innerText = chatPanel.classList.contains('collapsed') ? '▲' : '▼';
        if (!chatPanel.classList.contains('collapsed')) {
            chatInput.focus();
            scrollToBottom();
        }
    });

    // Chat - Enviar mensaje
    btnSendChat.addEventListener('click', sendChatMessage);
    chatInput.addEventListener('keypress', (e) => {
        if (e.key === 'Enter') sendChatMessage();
    });
}

// -----------------------------------------------------------------------------
// UI HELPERS
// -----------------------------------------------------------------------------
function actualizarVisibilidadCampos() {
    const prov = selectProvider.value;
    const groupApiKey = document.getElementById('group-api-key');
    const groupBaseUrl = document.getElementById('group-base-url');

    if (prov === 'ollama') {
        groupApiKey.style.display = 'none';
        groupBaseUrl.style.display = 'block';
    } else {
        groupApiKey.style.display = 'block';
        groupBaseUrl.style.display = 'none';
    }
}

function llenarSelectModelos(proveedor) {
    selectModel.innerHTML = '';
    const modelos = modelosPorProveedor[proveedor] || [];
    
    modelos.forEach(m => {
        const opt = document.createElement('option');
        opt.value = m.id;
        opt.innerText = m.nombre;
        selectModel.appendChild(opt);
    });
    
    // Opción personalizada
    const optCustom = document.createElement('option');
    optCustom.value = "custom";
    optCustom.innerText = "Personalizado...";
    selectModel.appendChild(optCustom);
    
    // Si la primera opción es 'custom', mostrar el input. Si no, ocultarlo.
    inputCustomModel.style.display = selectModel.value === 'custom' ? 'block' : 'none';
    
    selectModel.onchange = (e) => {
        inputCustomModel.style.display = e.target.value === 'custom' ? 'block' : 'none';
    };
}

// -----------------------------------------------------------------------------
// API CALLS
// -----------------------------------------------------------------------------
async function fetchConfig() {
    try {
        const res = await fetch(`${API_URL}/ai/config`);
        if (res.ok) {
            configActual = await res.json();
            
            // Reflejar en la UI
            selectProvider.value = configActual.proveedor || 'openai';
            llenarSelectModelos(selectProvider.value);
            
            if (configActual.modelo) {
                // Seleccionar modelo si existe en la lista, sino usar custom
                let found = Array.from(selectModel.options).some(o => o.value === configActual.modelo);
                if (found) {
                    selectModel.value = configActual.modelo;
                    inputCustomModel.style.display = 'none';
                } else {
                    selectModel.value = 'custom';
                    inputCustomModel.value = configActual.modelo;
                    inputCustomModel.style.display = 'block';
                }
            }
            
            if (configActual.base_url) inputBaseUrl.value = configActual.base_url;
            toggleAutoHealing.checked = configActual.auto_healing || false;

            if (configActual.tiene_api_key) {
                inputApiKey.placeholder = "•••••••••••••••• (API Key Guardada)";
            }

            if (configActual.configurado) {
                addChatMessage("system", `✅ Motor AIOps configurado (${configActual.proveedor})`);
            }
        }
    } catch (e) {
        console.error("Error fetching AI config", e);
    }
}

async function saveConfig() {
    const prov = selectProvider.value;
    const apiKey = inputApiKey.value.trim();
    let modelo = selectModel.value === 'custom' ? inputCustomModel.value.trim() : selectModel.value;
    const baseUrl = inputBaseUrl.value.trim();

    const yaTieneKey = configActual && configActual.tiene_api_key && configActual.proveedor === prov;
    if (prov !== 'ollama' && !apiKey && !yaTieneKey) {
        alert("La API Key es obligatoria para este proveedor.");
        return;
    }
    if (!modelo) {
        alert("Debes seleccionar o escribir un modelo.");
        return;
    }

    const btn = btnSaveConfig;
    btn.disabled = true;
    btn.innerText = "Guardando...";

    try {
        const res = await fetch(`${API_URL}/ai/config`, {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({
                proveedor: prov,
                api_key: apiKey,
                modelo: modelo,
                base_url: baseUrl,
                auto_healing: toggleAutoHealing.checked
            })
        });

        if (res.ok) {
            modalConfig.classList.remove('active');
            inputApiKey.value = ""; // Limpiar campo por seguridad
            await fetchConfig();
            addChatMessage("system", `✅ Motor AIOps conectado exitosamente a ${prov}.`);
        } else {
            const err = await res.json();
            alert(`Error: ${err.error}`);
        }
    } catch (e) {
        alert("Error de red guardando configuración.");
    } finally {
        btn.disabled = false;
        btn.innerText = "Guardar y Conectar";
    }
}

async function setAutoHealing(activo) {
    try {
        const res = await fetch(`${API_URL}/ai/auto-healing`, {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({ activo: activo })
        });
        
        if (res.ok) {
            addChatMessage("system", `🛡️ Guardián Autónomo ${activo ? 'ACTIVADO' : 'DESACTIVADO'}`);
        }
    } catch (e) {
        console.error("Error setting auto-healing", e);
    }
}

// -----------------------------------------------------------------------------
// CHAT LOGIC
// -----------------------------------------------------------------------------
async function sendChatMessage() {
    const text = chatInput.value.trim();
    if (!text) return;

    if (!configActual || !configActual.configurado) {
        alert("Por favor, configura el motor de IA primero (Botón IA en la barra superior).");
        return;
    }

    // Agregar mensaje del usuario a la UI
    addChatMessage("user", text);
    chatInput.value = "";
    
    // Mostrar indicador de "escribiendo..."
    const typingId = addChatMessage("system", "🧠 Analizando telemetría y razonando...");
    
    try {
        const res = await fetch(`${API_URL}/ai/chat`, {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({ mensaje: text })
        });

        removeChatMessage(typingId);

        if (res.ok) {
            const data = await res.json();
            
            // Mostrar los tool calls que hizo
            if (data.tool_calls_ejecutados > 0) {
                addChatMessage("tool", `⚙️ Se ejecutaron ${data.tool_calls_ejecutados} herramientas (Function Calls)`);
            }
            
            // Mostrar respuesta final
            addChatMessage("assistant", data.respuesta);
        } else {
            const err = await res.json();
            addChatMessage("error", `Error: ${err.error}`);
        }
    } catch (e) {
        removeChatMessage(typingId);
        addChatMessage("error", `Error de red de conexión con la IA.`);
    }
}

let historyPollInterval = null;

async function pollHistory() {
    if (!configActual || !configActual.configurado) return;
    
    try {
        const res = await fetch(`${API_URL}/ai/history`);
        if (res.ok) {
            const history = await res.json();
            if (history && history.length > lastHistoryCount) {
                // Hay nuevos eventos en el historial (ej: el Guardián actuó solo)
                for (let i = lastHistoryCount; i < history.length; i++) {
                    const entry = history[i];
                    if (entry.tipo === "auto_healing") {
                        // Expandir el chat si está cerrado para que el usuario vea
                        if (chatPanel.classList.contains('collapsed')) {
                            btnToggleChat.click();
                        }
                        
                        addChatMessage("system", "🚨 ALERTA DEL GUARDIÁN AIOPS");
                        addChatMessage("assistant", entry.resumen);
                        if (entry.tool_calls_ejecutados > 0) {
                            addChatMessage("tool", `⚙️ El Guardián ejecutó ${entry.tool_calls_ejecutados} herramientas para restaurar el sistema automáticamente.`);
                        }
                    } else if (entry.tipo === "auto_healing_error") {
                        addChatMessage("error", `❌ Error del Guardián: ${entry.resumen}`);
                    }
                }
                lastHistoryCount = history.length;
            }
        }
    } catch (e) {
        // Ignorar errores de polling
    }
}

let msgCounter = 0;
function addChatMessage(role, text) {
    const div = document.createElement('div');
    const id = `msg-${msgCounter++}`;
    div.id = id;
    div.className = `message ${role}`;
    
    // Markdown super básico (negritas y code blocks)
    let formattedText = text.replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>');
    formattedText = formattedText.replace(/`(.*?)`/g, '<code style="background:rgba(0,0,0,0.3);padding:2px 4px;border-radius:3px;">$1</code>');
    formattedText = formattedText.replace(/\n/g, '<br>');
    
    div.innerHTML = formattedText;
    chatMessages.appendChild(div);
    scrollToBottom();
    
    return id;
}

function removeChatMessage(id) {
    const el = document.getElementById(id);
    if (el) el.remove();
}

function scrollToBottom() {
    chatMessages.scrollTop = chatMessages.scrollHeight;
}

// Inicializar al cargar
document.addEventListener('DOMContentLoaded', initAI);

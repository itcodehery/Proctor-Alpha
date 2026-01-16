import * as monaco from 'monaco-editor';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import Split from 'split.js';

import "@xterm/xterm/css/xterm.css";

// --- Monaco Setup ---
self.MonacoEnvironment = {
    getWorkerUrl: function (_moduleId, label) {
        if (label === 'json') {
            return './node_modules/monaco-editor/esm/vs/language/json/json.worker.js';
        }
        if (label === 'css' || label === 'scss' || label === 'less') {
            return './node_modules/monaco-editor/esm/vs/language/css/css.worker.js';
        }
        if (label === 'html' || label === 'handlebars' || label === 'razor') {
            return './node_modules/monaco-editor/esm/vs/language/html/html.worker.js';
        }
        if (label === 'typescript' || label === 'javascript') {
            return './node_modules/monaco-editor/esm/vs/language/typescript/ts.worker.js';
        }
        return './node_modules/monaco-editor/esm/vs/editor/editor.worker.js';
    }
};

let monacoEditor = null;
const openFiles = new Map(); // fileName -> { model, state }
let activeFileName = null;

function initMonaco() {
    monacoEditor = monaco.editor.create(document.getElementById('monaco-container'), {
        theme: 'vs-dark',
        automaticLayout: true,
        fontFamily: 'JetBrains Mono',
        fontSize: 13,
        minimap: { enabled: false },
        lineNumbers: 'on',
        renderWhitespace: 'none',
        scrollBeyondLastLine: false,
        backgroundColor: '#141417'
    });

    // Define a custom theme to match our Pro aesthetic
    monaco.editor.defineTheme('proctor-theme', {
        base: 'vs-dark',
        inherit: true,
        rules: [],
        colors: {
            'editor.background': '#141417',
            'editor.lineHighlightBackground': '#1c1c21',
            'editorCursor.foreground': '#10b981',
            'editor.selectionBackground': '#10b98133',
        }
    });
    monaco.editor.setTheme('proctor-theme');

    // Auto-save on change
    monacoEditor.onDidChangeModelContent(() => {
        if (activeFileName) {
            const content = monacoEditor.getValue();
            invoke('write_file', { name: activeFileName, content });
        }
    });
}

// --- File Explorer & Tabs ---
async function refreshFileList() {
    try {
        const files = await invoke('list_files');
        const container = document.getElementById('file-list');
        container.innerHTML = '';
        
        files.forEach(file => {
            const item = document.createElement('div');
            item.className = `file-item ${file === activeFileName ? 'active' : ''}`;
            item.innerHTML = `<span class="icon">📄</span> ${file}`;
            item.onclick = () => openFile(file);
            container.appendChild(item);
        });
    } catch (e) {
        console.error('Failed to list files:', e);
    }
}

async function openFile(name) {
    if (activeFileName === name) return;

    if (!openFiles.has(name)) {
        try {
            const content = await invoke('read_file', { name });
            const extension = name.split('.').pop();
            let language = 'plaintext';
            
            // Basic language detection
            const langMap = {
                'js': 'javascript', 'ts': 'typescript', 'py': 'python',
                'c': 'c', 'cpp': 'cpp', 'html': 'html', 'css': 'css',
                'json': 'json', 'md': 'markdown', 'rs': 'rust'
            };
            language = langMap[extension] || 'plaintext';

            const model = monaco.editor.createModel(content, language);
            openFiles.set(name, { model });
            addTab(name);
        } catch (e) {
            console.error('Failed to read file:', e);
            return;
        }
    }

    activeFileName = name;
    const fileData = openFiles.get(name);
    monacoEditor.setModel(fileData.model);
    
    // Update UI
    document.querySelectorAll('.file-item').forEach(el => {
        el.classList.toggle('active', el.innerText.includes(name));
    });
    document.querySelectorAll('.tab').forEach(el => {
        el.classList.toggle('active', el.dataset.name === name);
    });

    monacoEditor.focus();
}

function addTab(name) {
    const tabBar = document.getElementById('tab-bar');
    const tab = document.createElement('div');
    tab.className = 'tab';
    tab.dataset.name = name;
    tab.innerHTML = `
        <span>${name}</span>
        <span class="tab-close">✕</span>
    `;
    
    tab.onclick = () => openFile(name);
    tab.querySelector('.tab-close').onclick = (e) => {
        e.stopPropagation();
        closeFile(name);
    };
    
    tabBar.appendChild(tab);
}

function closeFile(name) {
    if (openFiles.has(name)) {
        const fileData = openFiles.get(name);
        fileData.model.dispose();
        openFiles.delete(name);
        
        const tab = document.querySelector(`.tab[data-name="${name}"]`);
        if (tab) tab.remove();

        if (activeFileName === name) {
            const remaining = Array.from(openFiles.keys());
            if (remaining.length > 0) {
                openFile(remaining[remaining.length - 1]);
            } else {
                activeFileName = null;
                monacoEditor.setModel(null);
            }
        }
    }
}

document.getElementById('refresh-files-btn').onclick = () => refreshFileList();

document.getElementById('new-file-btn').onclick = async () => {
    const fileName = prompt('Enter file name:');
    if (fileName) {
        try {
            await invoke('create_file', { name: fileName });
            await refreshFileList();
            openFile(fileName);
        } catch (e) {
            alert(e);
        }
    }
};

// --- Terminal Factory ---
function createTerminal(containerId, ptyId) {
    const term = new Terminal({
        theme: {
            background: '#141417',
            foreground: '#f8fafc',
            cursor: '#10b981',
            selectionBackground: 'rgba(16, 185, 129, 0.3)',
        },
        fontSize: 13,
        fontFamily: 'JetBrains Mono',
        cursorBlink: true,
        altClickMovesCursor: false,
        lineHeight: 1.2,
    });

    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);

    const container = document.getElementById(containerId);
    term.open(container);

    // Initial fit
    setTimeout(() => fitAddon.fit(), 100);

    // Handle input from terminal
    term.onData((data) => {
        const bytes = new TextEncoder().encode(data);
        invoke("write_to_pty", {
            ptyId: ptyId,
            data: Array.from(bytes)
        });
    });

    // Custom key handler to capture shortcuts
    term.attachCustomKeyEventHandler((e) => {
        if (e.type === 'keydown') {
            // Alt + Shift + S (Toggle)
            if (e.altKey && e.shiftKey && e.code === 'KeyS') {
                if (ptyId === 'terminal') {
                    monacoEditor.focus();
                } else {
                    shell.term.focus();
                }
                return false;
            }
            // Alt + Shift + E (Focus Editor)
            if (e.altKey && e.shiftKey && e.code === 'KeyE') {
                monacoEditor.focus();
                return false;
            }
            // Alt + Shift + T (Focus Terminal)
            if (e.altKey && e.shiftKey && e.code === 'KeyT') {
                shell.term.focus();
                return false;
            }
        }
        return true;
    });

    return { term, fitAddon };
}

// --- Initialize Components ---
initMonaco();
const shell = createTerminal('shell-container', 'terminal');

refreshFileList();

// --- Global Shortcut Listener ---
window.addEventListener('keydown', (e) => {
    if (e.altKey && e.shiftKey && e.code === 'KeyS') {
        e.preventDefault();
        if (document.activeElement.closest('.monaco-instance')) {
            shell.term.focus();
        } else {
            monacoEditor.focus();
        }
    }
    if (e.altKey && e.shiftKey && e.code === 'KeyE') {
        e.preventDefault();
        monacoEditor.focus();
    }
    if (e.altKey && e.shiftKey && e.code === 'KeyT') {
        e.preventDefault();
        shell.term.focus();
    }
});

// --- Listen for PTY output from Rust ---
listen("pty-output", (event) => {
    const { pty_id, data } = event.payload;
    const bytes = new Uint8Array(data);
    if (pty_id === 'terminal') {
        shell.term.write(bytes);
    }
});

// --- Handle Global Resizes ---
window.addEventListener('resize', () => {
    shell.fitAddon.fit();
});

// --- Split.js Initialization ---
Split(['#col-left', '#col-right'], {
    sizes: [72, 28],
    minSize: 200,
    gutterSize: 6,
    onDrag: () => {
        shell.fitAddon.fit();
    }
});

Split(['#pane-editor', '#pane-terminal'], {
    direction: 'vertical',
    sizes: [65, 35],
    minSize: 100,
    gutterSize: 6,
    onDrag: () => {
        shell.fitAddon.fit();
    }
});

// --- Session Timer ---
let sessionSeconds = 0;
let timerInterval = null;
const sessionTimerDisplay = document.querySelector('.session-timer');

function formatTime(totalSeconds) {
    const mins = Math.floor(totalSeconds / 60);
    const secs = totalSeconds % 60;
    return `${mins.toString().padStart(2, '0')}:${secs.toString().padStart(2, '0')}`;
}

function startTimer() {
    if (timerInterval) return;
    timerInterval = setInterval(() => {
        sessionSeconds++;
        sessionTimerDisplay.innerText = formatTime(sessionSeconds);
    }, 1000);
}

startTimer();

// --- Process Shield (Go Backend Integration) ---
async function pollProcessShield() {
    try {
        const response = await fetch('http://localhost:8080/scan');
        if (!response.ok) return; 
        const data = await response.json();
        
        if (data.forbidden_found) {
            const apps = data.processes.join(', ');
            // Check if we just logged this to avoid spamming? 
            // For now, simple logging.
            addLogEntry('alert', `Process Shield: Detected ${apps}`);
        }
    } catch (e) {
        // Backend likely offline
    }
}

setInterval(pollProcessShield, 5000);

// --- Dialog Management ---
const endSessionBtn = document.getElementById('end-session-btn');
const dialogOverlay = document.getElementById('dialog-overlay');
const closeIcon = document.querySelector('.modal-close-icon');
const appContainer = document.getElementById('app');
const adminInput = document.getElementById('admin-input');

const activeIndicators = document.querySelectorAll('.active-only');
const pausedIndicators = document.querySelectorAll('.paused-only');

function setSessionState(isPaused) {
    if (isPaused) {
        appContainer.classList.add('paused-mode');
        dialogOverlay.style.display = 'flex';
        activeIndicators.forEach(el => el.style.display = 'none');
        pausedIndicators.forEach(el => el.style.display = 'flex');
        adminInput.focus();
    } else {
        appContainer.classList.remove('paused-mode');
        dialogOverlay.style.display = 'none';
        activeIndicators.forEach(el => el.style.display = 'flex');
        pausedIndicators.forEach(el => el.style.display = 'none');
        adminInput.value = '';
    }
}

endSessionBtn.addEventListener('click', () => setSessionState(true));
closeIcon.addEventListener('click', () => setSessionState(false));

dialogOverlay.addEventListener('click', (e) => {
    if (e.target === dialogOverlay) setSessionState(false);
});

async function exportLog() {
    const entries = Array.from(document.querySelectorAll('.log-entry')).map(entry => {
        const time = entry.querySelector('.log-time').innerText;
        const task = entry.querySelector('.log-task').innerText;
        return `[${time}] ${task}`;
    }).join('\n');

    try {
        await invoke('save_log', { logContent: entries });
        return true;
    } catch (e) {
        console.error('Failed to save log:', e);
        return false;
    }
}

adminInput.addEventListener('keydown', async (e) => {
    if (e.key === 'Enter') {
        const key = adminInput.value;
        const isValid = await invoke('verify_admin_key', { adminKey: key });
        if (isValid) {
            await exportLog();
            await invoke('exit_app');
        } else {
            adminInput.classList.add('error');
            setTimeout(() => adminInput.classList.remove('error'), 500);
        }
    }
});

listen('attempted-close', () => {
    setSessionState(true);
});

// --- Logger Logic ---
const logEntriesContainer = document.getElementById('log-entries');

function addLogEntry(type, message) {
    const now = new Date();
    const timeString = now.toLocaleTimeString([], { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });

    const entryDiv = document.createElement('div');
    entryDiv.className = 'log-entry';
    entryDiv.setAttribute('data-type', type);
    
    entryDiv.innerHTML = `
        <div class="log-header">
            <span class="log-type-tag">${type}</span>
            <span class="log-time">${timeString}</span>
        </div>
        <div class="log-task">${message}</div>
    `;
    
    logEntriesContainer.appendChild(entryDiv);
    logEntriesContainer.scrollTop = logEntriesContainer.scrollHeight;
}

listen('log-event', (event) => {
    const { type, message } = event.payload;
    addLogEntry(type === 'command' ? 'command' : 'file', message);
    if (type === 'file') {
        refreshFileList();
    }
});

const exportLogBtn = document.querySelector('.export-log-btn');
if (exportLogBtn) {
    exportLogBtn.addEventListener('click', async () => {
        const success = await exportLog();
        if (success) {
            exportLogBtn.innerText = "Log Exported";
            setTimeout(() => exportLogBtn.innerText = "Export Session Log", 3000);
        }
    });
}

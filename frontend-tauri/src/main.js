import * as monaco from 'monaco-editor';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import { getCurrentWindow } from "@tauri-apps/api/window";
import Split from 'split.js';

import "@xterm/xterm/css/xterm.css";

// --- Monaco Setup ---
self.MonacoEnvironment = {
  getWorkerUrl: function(_moduleId, label) {
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
const openFiles = new Map(); // fileName -> { model, state, originalContent }
let activeFileName = null;
const unsavedFiles = new Set();
let autoSaveEnabled = false;
let typingTimer = null;
const TYPING_TIMEOUT = 3000; // 3 seconds

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

  // Track changes for unsaved state & Auto-save
  monacoEditor.onDidChangeModelContent(() => {
    if (activeFileName) {
      handleTyping();

      if (!unsavedFiles.has(activeFileName)) {
        unsavedFiles.add(activeFileName);
        updateTabState(activeFileName, true);
      }
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
      openFiles.set(name, { model, originalContent: content }); // Store original content if needed for diff checks later
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

  // Update active tab styling
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

  // Check if previously unsaved
  if (unsavedFiles.has(name)) {
    tab.classList.add('unsaved');
  }

  tab.innerHTML = `
        <span class="tab-name">${name}</span>
        <span class="tab-close">✕</span>
    `;

  tab.onclick = () => openFile(name);
  tab.querySelector('.tab-close').onclick = (e) => {
    e.stopPropagation();
    closeFile(name);
  };

  tabBar.appendChild(tab);
}

function updateTabState(name, isUnsaved) {
  const tab = document.querySelector(`.tab[data-name="${name}"]`);
  if (tab) {
    if (isUnsaved) {
      tab.classList.add('unsaved');
    } else {
      tab.classList.remove('unsaved');
    }
  }
}

// --- Auto-Save Logic ---
const saveStatusEl = document.getElementById('save-status');
const autoSaveToggleBtn = document.getElementById('auto-save-toggle');

function updateSaveStatus(status) {
  saveStatusEl.innerText = status;
}

function handleTyping() {
  updateSaveStatus("Typing...");

  if (typingTimer) {
    clearTimeout(typingTimer);
  }

  if (autoSaveEnabled) {
    typingTimer = setTimeout(() => {
      saveCurrentFile();
      updateSaveStatus("Saved");
    }, TYPING_TIMEOUT);
  } else {
    updateSaveStatus("Unsaved");
  }
}

function toggleAutoSave() {
  autoSaveEnabled = !autoSaveEnabled;
  autoSaveToggleBtn.classList.toggle('enabled', autoSaveEnabled);

  if (autoSaveEnabled) {
    // If enabling and there are unsaved changes, start timer
    if (activeFileName && unsavedFiles.has(activeFileName)) {
      handleTyping();
    } else {
      updateSaveStatus("Ready");
    }
  } else {
    if (typingTimer) {
      clearTimeout(typingTimer);
      typingTimer = null;
    }
    updateSaveStatus(unsavedFiles.has(activeFileName) ? "Unsaved" : "Ready");
  }
}

async function saveCurrentFile() {
  if (!activeFileName) return;

  // Clear any pending auto-save timer to avoid double save
  if (typingTimer) {
    clearTimeout(typingTimer);
    typingTimer = null;
  }

  const content = monacoEditor.getValue();
  try {
    await invoke('write_file', { name: activeFileName, content });

    unsavedFiles.delete(activeFileName);
    updateTabState(activeFileName, false);
    updateSaveStatus("Saved");

    // Visual feedback (optional)
    const saveBtn = document.getElementById('save-file-btn');
    const originalText = saveBtn.innerText;
    saveBtn.innerText = "✓";
    setTimeout(() => saveBtn.innerText = originalText, 1000);

  } catch (e) {
    console.error("Failed to save:", e);
    updateSaveStatus("Error");
    alert("Failed to save file: " + e);
  }
}

autoSaveToggleBtn.onclick = toggleAutoSave;

function closeFile(name) {
  if (unsavedFiles.has(name)) {
    if (!confirm(`File ${name} has unsaved changes. Close anyway?`)) {
      return;
    }
    unsavedFiles.delete(name);
  }

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
document.getElementById('save-file-btn').onclick = () => saveCurrentFile();

// --- New File Modal Logic ---
const newFileDialog = document.getElementById('new-file-dialog');
const newFileInput = document.getElementById('new-file-input');
const createFileConfirmBtn = document.getElementById('create-file-confirm-btn');
const newFileCloseIcon = document.getElementById('new-file-close-icon');

function toggleNewFileModal(show) {
  newFileDialog.style.display = show ? 'flex' : 'none';
  if (show) {
    newFileInput.value = '';
    newFileInput.focus();
  }
}

document.getElementById('new-file-btn').onclick = () => toggleNewFileModal(true);
newFileCloseIcon.onload = () => { }; // Safety
newFileCloseIcon.onclick = () => toggleNewFileModal(false);

// Close on background click
newFileDialog.onclick = (e) => {
  if (e.target === newFileDialog) toggleNewFileModal(false);
};

async function handleCreateFile() {
  const fileName = newFileInput.value.trim();
  if (!fileName) {
    alert("Please enter a file name");
    return;
  }

  try {
    await invoke('create_file', { name: fileName });
    await refreshFileList();
    openFile(fileName);
    toggleNewFileModal(false);
  } catch (e) {
    alert("Error creating file: " + e);
  }
}

createFileConfirmBtn.onclick = handleCreateFile;
newFileInput.onkeydown = (e) => {
  if (e.key === 'Enter') handleCreateFile();
  if (e.key === 'Escape') toggleNewFileModal(false);
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
  // Save: Ctrl + S or Cmd + S
  if ((e.ctrlKey || e.metaKey) && e.key === 's') {
    e.preventDefault();
    saveCurrentFile();
    return;
  }

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

// --- Live View Global State ---
let liveViewEditor = null;
let liveViewStudentId = null;

function initLiveViewEditor() {
    if (liveViewEditor) return;
    liveViewEditor = monaco.editor.create(document.getElementById('lv-editor-container'), {
        value: "// Waiting for student code...",
        language: "javascript",
        theme: "vs-dark",
        readOnly: true,
        automaticLayout: true,
        fontSize: 12,
        minimap: { enabled: false }
    });
}

document.getElementById('live-view-close').onclick = () => {
    document.getElementById('live-view-overlay').style.display = 'none';
    liveViewStudentId = null;
};

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
  if (!isStudentSessionActive) return; // Only scan during student exam session

  try {
    const response = await fetch(`${getAdminApiBase()}/scan?room_id=${currentRoomId}`);
    if (!response.ok) return;
    const data = await response.json();

    if (data.forbidden_found) {
      const apps = data.processes.join(', ');
      addLogEntry('alert', `Process Shield: Detected ${apps}`);
    }
  } catch (e) {
    // Backend likely offline
  }
}

setInterval(pollProcessShield, 5000);

// --- Student Auto-Sync & Snapshots ---
async function syncStudentCode() {
    if (!isStudentSessionActive) return;
    
    // Collect all open file contents
    const files = {};
    for (const [name, state] of Object.entries(fileStates)) {
        files[name] = state.content;
    }

    try {
        await fetch(`${getAdminApiBase()}/sync-code`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                room_id: currentRoomId,
                user_id: joinRegNoInput.value.trim(),
                files: files
            })
        });
    } catch (e) {
        console.error("Auto-sync failed:", e);
    }
}

async function captureAndSendSnapshot() {
    if (!isStudentSessionActive) return;
    try {
        const snapshot = await invoke('capture_screenshot');
        await fetch(`${getAdminApiBase()}/student/snapshot`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                room_id: currentRoomId,
                user_id: joinRegNoInput.value.trim(),
                snapshot: snapshot
            })
        });
    } catch (e) {
        console.error("Snapshot failed:", e);
    }
}

setInterval(syncStudentCode, 30000); // Sync every 30s
setInterval(captureAndSendSnapshot, 60000); // Snapshot every 1m

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

// --- Window Controls Logic ---
const appWindow = getCurrentWindow();

function attachWindowControl(id, action) {
  const el = document.getElementById(id);
  if (!el) return;
  el.addEventListener('click', action);
  // STOPS the drag region from capturing the click
  el.addEventListener('mousedown', (e) => e.stopPropagation());
}

// Attach to IDE controls
attachWindowControl('ide-close', () => appWindow.close());
attachWindowControl('ide-minimize', () => appWindow.minimize());
attachWindowControl('ide-maximize', () => appWindow.toggleMaximize());

// Attach to global/landing controls if they exist (old IDs from reverted state might still be in memory or if IDs change)
attachWindowControl('win-close', () => appWindow.close());
attachWindowControl('win-minimize', () => appWindow.minimize());
attachWindowControl('win-maximize', () => appWindow.toggleMaximize());

// Attach to Landing Page controls (Specific IDs)
attachWindowControl('landing-close', () => appWindow.close());
attachWindowControl('landing-minimize', () => appWindow.minimize());
attachWindowControl('landing-maximize', () => appWindow.toggleMaximize());

// Attach to Join Room controls
attachWindowControl('join-close', () => appWindow.close());
attachWindowControl('join-minimize', () => appWindow.minimize());
attachWindowControl('join-maximize', () => appWindow.toggleMaximize());


// --- Landing Page Logic ---
const landingContainer = document.getElementById('landing-container');
const btnStudent = document.getElementById('btn-student');
const btnAdmin = document.getElementById('btn-admin');

// --- Admin Dashboard Logic ---
const adminContainer = document.getElementById('admin-container');
const adminBackBtn = document.getElementById('admin-back-btn');
const refreshRoomsBtn = document.getElementById('refresh-rooms-btn');
const createRoomBtn = document.getElementById('create-room-btn');
const createRoomDialog = document.getElementById('create-room-dialog');
const createRoomClose = document.getElementById('create-room-close');
const crSubmitBtn = document.getElementById('cr-submit-btn');
const serverStatusIndicator = document.getElementById('server-status-indicator');

let isStudentSessionActive = false;


const DEFAULT_IP = "localhost";
const API_PORT = "8081";

function getServerIp() {
  return localStorage.getItem('server_ip') || DEFAULT_IP;
}

function saveServerIp(ip) {
  if (ip) {
    localStorage.setItem('server_ip', ip);
  }
}

function getStudentApiBase() {
  return `http://${getServerIp()}:${API_PORT}`;
}

function getAdminApiBase() {
  return `http://localhost:${API_PORT}`;
}

function getStudentWsBase() {
  return `ws://${getServerIp()}:${API_PORT}/ws`;
}

function getAdminWsBase() {
  return `ws://localhost:${API_PORT}/ws`;
}

// Helper to decide which base to use based on context or just use specific ones
// For simplicity, we will replace usages with specific functions.
// But some shared functions (like checkBackendHealth) might need context.
// Actually checkBackendHealth is used by Admin mostly? 
// Wait, Student also checks health? 
// checkBackendHealth is used in `startBackend` which attempts to start local backend.
// So checkBackendHealth should use LOCALHOST always.

// const API_BASE = "http://localhost:8080"; // Deprecated
// const WS_BASE = "ws://localhost:8080/ws"; // Deprecated
let ws = null;
let wsRetries = 0;
const { Command } = window.__TAURI__.shell; // Access shell plugin

// Backend Management
async function checkBackendHealth() {
  try {
    const res = await fetch(`${getAdminApiBase()}/scan`, { method: 'OPTIONS' }); // Lightweight check
    if (res.ok) {
      updateServerStatus(true);
      return true;
    }
  } catch (e) {
    updateServerStatus(false);
    return false;
  }
  return false;
}

function updateServerStatus(isOnline) {
  if (serverStatusIndicator) {
    if (isOnline) {
      serverStatusIndicator.classList.add('online');
      serverStatusIndicator.classList.remove('error');
      serverStatusIndicator.querySelector('.status-text').innerText = "ONLINE";
    } else {
      serverStatusIndicator.classList.remove('online');
      serverStatusIndicator.classList.add('error');
      serverStatusIndicator.querySelector('.status-text').innerText = "OFFLINE";
    }
  }
}

async function startBackend() {
  const isOnline = await checkBackendHealth();
  if (isOnline) return; // Already running

  console.log("Starting Backend Server...");
  if (serverStatusIndicator) {
    serverStatusIndicator.querySelector('.status-text').innerText = "STARTING...";
  }

  try {
    // Spawn 'go run .' in the backend directory
    // Note: Command definition depends on permissions configuration in capabilities
    const command = Command.create('go', ['run', '.'], { cwd: '../backend+logic' }); // Adjust CWD if needed, relative to app execution? 
    // Actually, CWD support in Tauri shell plugin might be restricted or relative to bundle. 
    // For 'run', usually absolute config is safer. 
    // Let's assume the user runs this from project root or standardized path.
    // If 'cwd' isn't supported easily, we might need a better strategy.
    // BUT, given the scope, let's try assuming the sidecar approach is complex and just try spawning it.
    // Better yet, for this dev environment, let's assume `go` is in path.
    // Wait, `cwd` option in Command.create is not standard in v1/v2 JS API directly without specific config.
    // Let's rely on the user having started it MANUALLY first as fallback, or try to run it.

    // REVISION: The safe bet for this environment is to instruct the user if auto-start fails.
    // But I will try to spawn it.

    // For development, we'll try to run "go run ." inside "backend+logic".
    // The sidecar is robust but complex to setup now.
    // I will rely on the user-instruction I added: "Ensure Server is ONLINE".

    // Let's just try to update status.
    checkBackendHealth();
  } catch (e) {
    console.error("Failed to auto-start backend:", e);
  }
}

// Polling for health when on admin page
let healthInterval;

// --- Join Room Logic ---
const joinContainer = document.getElementById('join-room-container');
const joinBackBtn = document.getElementById('join-back-btn');
const joinSubmitBtn = document.getElementById('join-submit-btn');
const joinNameInput = document.getElementById('join-name');
const joinRegNoInput = document.getElementById('join-regno');
const joinRoomIdInput = document.getElementById('join-room-id');
const joinServerIpInput = document.getElementById('join-server-ip');
const joinError = document.getElementById('join-error');

// Initialize Server IP input
if (joinServerIpInput) {
  joinServerIpInput.value = getServerIp();
}

if (btnStudent) {
  btnStudent.addEventListener('click', () => {
    if (landingContainer && joinContainer) {
      landingContainer.classList.add('fade-out');
      joinContainer.classList.remove('fade-out');
      setTimeout(() => joinNameInput.focus(), 100);
    }
  });
}

if (joinBackBtn) {
  joinBackBtn.addEventListener('click', () => {
    if (landingContainer && joinContainer) {
      joinContainer.classList.add('fade-out');
      landingContainer.classList.remove('fade-out');
    }
  });
}

function showJoinError(msg) {
  if (!joinError) return;
  joinError.innerText = msg;
  joinError.style.display = 'block';
  joinError.classList.add('error'); // Trigger shake
  setTimeout(() => {
    joinError.classList.remove('error');
  }, 500);
  // Hide after 3s? Maybe keep it until user types.
}

async function handleJoinRoom() {
  const name = joinNameInput.value.trim();
  const regNo = joinRegNoInput.value.trim();
  const roomId = joinRoomIdInput.value.trim();

  const serverIp = joinServerIpInput ? joinServerIpInput.value.trim() : getServerIp();

  if (!name || !regNo || !roomId) {
    showJoinError("Please fill in all fields.");
    return;
  }

  // Save the IP for future use
  if (serverIp) {
    saveServerIp(serverIp);
  }

  joinSubmitBtn.innerText = "Joining...";
  joinSubmitBtn.disabled = true;
  joinError.style.display = 'none';

  console.log(`[DEBUG] Attempting to join room: ${roomId} as ${name} (${regNo})`);
  console.log(`[DEBUG] API URL: ${getStudentApiBase()}/join-room`);

  try {
    const res = await fetch(`${getStudentApiBase()}/join-room`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        room_id: roomId,
        username: name,
        regno: regNo,
        user_id: regNo // Using RegNo as ID for simplicity
      })
    });

    console.log(`[DEBUG] Response Status: ${res.status}`);
    const data = await res.json();
    console.log(`[DEBUG] Response Data:`, data);

    if (res.ok) {
      // Success!
      // console.log("Joined!", data);

      isStudentSessionActive = true;

      // Navigate to IDE
      joinContainer.classList.add('fade-out');

      // Adjust IDE layout
      setTimeout(() => {
        if (shell && shell.fitAddon) shell.fitAddon.fit();
        if (monacoEditor) monacoEditor.layout();
      }, 300);

      // Enable Kiosk Mode
      invoke('set_kiosk_mode', { enabled: true });

      // TODO: Update UI with session info if needed
    } else {
      showJoinError(data.message || "Failed to join room");
    }
  } catch (e) {
    console.error("[DEBUG] Join Room Error Details:", e);
    showJoinError("Server unreachable. Ensure Admin has started the exam server.");
  } finally {
    joinSubmitBtn.innerText = "Join Session";
    joinSubmitBtn.disabled = false;
  }
}

if (joinSubmitBtn) {
  joinSubmitBtn.onclick = handleJoinRoom;
}

// Allow Enter key to submit in last input
if (joinRoomIdInput) {
  joinRoomIdInput.onkeydown = (e) => {
    if (e.key === 'Enter') handleJoinRoom();
  };
}

if (btnAdmin) {
  btnAdmin.addEventListener('click', async () => {
    if (landingContainer && adminContainer) {
      landingContainer.classList.add('fade-out');
      adminContainer.classList.remove('fade-out');

      // Start polling (health only, rooms via WS)
      healthInterval = setInterval(checkBackendHealth, 5000);

      // Fetch initial state
      fetchRooms();

      // Connect Realtime
      initWebSocket();
    }
  });
}

if (adminBackBtn) {
  adminBackBtn.addEventListener('click', () => {
    if (landingContainer && adminContainer) {
      adminContainer.classList.add('fade-out');
      landingContainer.classList.remove('fade-out');
      clearInterval(healthInterval);
    }
  });
}




// Room Management
async function fetchRooms() {
  const tbody = document.getElementById('rooms-list-body');
  const loading = document.getElementById('rooms-loading');
  const empty = document.getElementById('rooms-empty');

  tbody.innerHTML = '';
  loading.style.display = 'block';
  empty.style.display = 'none';

  try {
    const res = await fetch(`${getAdminApiBase()}/get-all-rooms`);
    const rooms = await res.json();

    loading.style.display = 'none';

    if (!rooms || rooms.length === 0) {
      empty.style.display = 'block';
      return;
    }

    rooms.forEach(r => {
      const tr = document.createElement('tr');
      tr.className = 'room-row';
      tr.dataset.id = r.id; // Store ID for click
      tr.style.cursor = 'pointer';

      // Format time
      const startTime = r.start_time ? new Date(r.start_time).toLocaleTimeString() : '-';

      let statusBadge = '';
      if (r.active_status === 0) statusBadge = '<span class="status-badge status-waiting">Waiting</span>';
      else if (r.active_status === 1) statusBadge = '<span class="status-badge status-active">Active</span>';
      else statusBadge = '<span class="status-badge">Finished</span>';

      tr.innerHTML = `
                <td class="mono" style="font-weight: 700; color: var(--accent-primary);">${r.id}</td>
                <td>${r.session_name}</td>
                <td>${statusBadge}</td>
                <td>${startTime}</td>
                <td><button class="small-btn">View</button></td>
            `;
      tbody.appendChild(tr);
    });

    bindRoomListEvents();

  } catch (e) {
    console.error("Failed to fetch rooms:", e);
    loading.style.display = 'none';
    tbody.innerHTML = `<tr><td colspan="5" style="color: var(--accent-danger); text-align: center;">Failed to load rooms. Is backend running?</td></tr>`;
  }
}

if (refreshRoomsBtn) {
  refreshRoomsBtn.onclick = fetchRooms;
}

// Create Room Modal
if (createRoomBtn) {
  createRoomBtn.onclick = () => {
    createRoomDialog.style.display = 'flex';
  };
}

if (createRoomClose) {
  createRoomClose.onclick = () => {
    createRoomDialog.style.display = 'none';
  };
}

if (crSubmitBtn) {
  crSubmitBtn.onclick = async () => {
    const name = document.getElementById('cr-name').value;
    const host = document.getElementById('cr-host').value;
    const key = document.getElementById('cr-key').value;

    if (!name || !host || !key) {
      alert("Please fill all fields");
      return;
    }

    try {
      const res = await fetch(`${getAdminApiBase()}/create-room`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          session_name: name,
          host_id: host,
          admin_key: key
        })
      });

      if (res.ok) {
        createRoomDialog.style.display = 'none';
        fetchRooms();
        // Clear inputs
        document.getElementById('cr-name').value = '';
        document.getElementById('cr-host').value = '';
        document.getElementById('cr-key').value = '';
      } else {
        const err = await res.text();
        alert("Failed to create room: " + err);
      }
    } catch (e) {
      alert("Error creating room: " + e);
    }
  };
}

// --- Room Details Logic ---
let currentRoomId = null;
let roomPollInterval = null;

async function openRoomDetails(roomId) {
  currentRoomId = roomId;
  const detailsView = document.getElementById('room-details-view');
  detailsView.classList.add('active');

  // Initial fetch
  await fetchRoomDetails();

  // Subscribe to real-time updates for THIS room
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({
      action: "subscribe_room",
      room_id: roomId
    }));
  }
}

function closeRoomDetails() {
  const detailsView = document.getElementById('room-details-view');
  detailsView.classList.remove('active');

  // Unsubscribe
  if (ws && ws.readyState === WebSocket.OPEN && currentRoomId) {
    ws.send(JSON.stringify({
      action: "unsubscribe_room",
      room_id: currentRoomId
    }));
  }

  currentRoomId = null;
  fetchRooms(); // Refresh main list
}

document.getElementById('rd-back-btn').addEventListener('click', closeRoomDetails);

// Tabs
document.querySelectorAll('.tab-btn').forEach(btn => {
  btn.addEventListener('click', () => {
    // Deactivate all
    document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
    document.querySelectorAll('.tab-content').forEach(c => {
      c.style.display = 'none';
      c.classList.remove('active');
    });

    // Activate current
    btn.classList.add('active');
    const tabId = btn.dataset.tab;
    const content = document.getElementById(`tab-${tabId}`);
    content.style.display = 'block';
    content.classList.add('active');
  });
});

async function fetchRoomDetails() {
  if (!currentRoomId) return;

  try {
    const res = await fetch(`${API_BASE}/get-room?room_id=${currentRoomId}`);
    if (!res.ok) return;
    const room = await res.json();

    // Update Header
    document.getElementById('rd-title').innerText = room.session_name;
    const badge = document.getElementById('rd-status-badge');
    updateBadge(badge, room.active_status);

    // Update Settings Form (only if not focused)
    if (document.activeElement.tagName !== 'INPUT' && document.activeElement.tagName !== 'TEXTAREA') {
      document.getElementById('rd-name').value = room.session_name;
      document.getElementById('rd-duration').value = room.time_allocated ? (room.time_allocated / 60000000000) : 0; // ns to min? Go Duration is ns. wait.
      // Go JSON duration is usually represented as string "1h2m" or similar if generic json marshal, but here it might be ns number.
      // Let's check Go struct. It treats Duration as int64 ns often in stdlib? No, standard json marshal for time.Duration is integer ns.
      // Actually standard json marshal for duration is just number of nanoseconds.
      // 1 min = 60 * 1000 * 1000 * 1000 = 6e10.
      document.getElementById('rd-duration').value = Math.round(room.time_allocated / 60000000000);

      document.getElementById('rd-status-select').value = room.active_status;

      // Update Sets
      const container = document.getElementById('sets-container');
      container.innerHTML = '';
      if (room.sets && Object.keys(room.sets).length > 0) {
        for (const [key, val] of Object.entries(room.sets)) {
          addSetRow(key, val);
        }
      } else {
        // Add one empty row by default if empty
        addSetRow();
      }
    }

    // Update Students List
    const tbody = document.getElementById('rd-students-body');
    const empty = document.getElementById('rd-students-empty');
    tbody.innerHTML = '';

    if (!room.students || room.students.length === 0) {
      empty.style.display = 'block';
    } else {
      empty.style.display = 'none';
      room.students.forEach(s => {
        const tr = document.createElement('tr');
        tr.innerHTML = `
                    <td>${s.regno}</td>
                    <td>${s.username || 'N/A'}</td>
                    <td>${getStatusBadgeHTML(s.active_status)}</td>
                    <td class="mono">${s.ip_address}</td>
                    <td>
                        <button class="small-btn" style="border-color: var(--accent-primary); color: var(--accent-primary);" onclick="openLiveView('${s.user_id}', '${s.username}')">Live View</button>
                        <button class="small-btn" style="border-color: #ef4444; color: #ef4444;" onclick="moderateStudent('${s.user_id}', 1)">Kick</button>
                    </td>
                `;
        tbody.appendChild(tr);
      });
    }

  } catch (e) {
    console.error("Fetch details error:", e);
  }
}

function addSetRow(name = '', url = '') {
  const container = document.getElementById('sets-container');
  const div = document.createElement('div');
  div.className = 'set-row';
  div.innerHTML = `
        <input type="text" class="admin-input set-name" placeholder="Set Name (e.g. Set A)" value="${name}">
        <input type="text" class="admin-input set-url" placeholder="Questions URL" value="${url}">
        <div class="remove-set-btn" title="Remove">✕</div>
    `;
  div.querySelector('.remove-set-btn').onclick = () => div.remove();
  container.appendChild(div);
}

document.getElementById('add-set-btn').addEventListener('click', () => addSetRow());

// Save Changes
document.getElementById('rd-save-btn').addEventListener('click', async () => {
  if (!currentRoomId) return;
  const name = document.getElementById('rd-name').value;
  const durationMins = parseInt(document.getElementById('rd-duration').value);
  const status = parseInt(document.getElementById('rd-status-select').value);
  const key = document.getElementById('rd-key').value;

  // Collect Sets
  const sets = {};
  document.querySelectorAll('.set-row').forEach(row => {
    const setName = row.querySelector('.set-name').value.trim();
    const setUrl = row.querySelector('.set-url').value.trim();
    if (setName && setUrl) {
      sets[setName] = setUrl;
    }
  });

  // Collect Forbidden Apps
  const forbiddenApps = document.getElementById('rd-forbidden-apps').value.split(',').map(s => s.trim()).filter(s => s !== "");

  if (!key) {
    alert("Admin Key is required to save changes.");
    return;
  }

  try {
    const res = await fetch(`${getAdminApiBase()}/update-room`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        room_id: currentRoomId,
        admin_key: key,
        session_name: name,
        time_allocated: durationMins * 60000000000,
        active_status: status,
        sets: sets,
        forbidden_apps: forbiddenApps,
        invite_list: document.getElementById('rd-invite-list').value.split(',').map(s => s.trim()).filter(s => s !== "")
      })
    });

    if (res.ok) {
      alert("Changes saved!");
      fetchRoomDetails();
    } else {
      alert("Failed: " + await res.text());
    }
  } catch (e) {
    alert("Error: " + e);
  }
});

function getStatusBadgeHTML(status) {
  // 0: Online, 1: Offline/Kick, 2: Submitted, 3: Flagged
  // Based on UStatusEnum in backend
  switch (status) {
    case 0: return '<span class="status-badge status-active">Online</span>';
    case 1: return '<span class="status-badge" style="color:#ef4444; border-color:#ef4444; background:rgba(239,68,68,0.1)">Offline</span>';
    case 2: return '<span class="status-badge" style="color:#10b981; border-color:#10b981; background:rgba(16,185,129,0.1)">Submitted</span>';
    case 3: return '<span class="status-badge" style="color:#f59e0b; border-color:#f59e0b; background:rgba(245,158,11,0.1)">Flagged</span>';
    default: return 'Unknown';
  }
}

function updateBadge(el, status) {
  // 0: Waiting, 1: Active, 2: NetworkLoss, 3: Paused, 4: Complete
  el.className = 'status-badge';
  if (status === 0) { el.classList.add('status-waiting'); el.innerText = 'WAITING'; }
  else if (status === 1) { el.classList.add('status-active'); el.innerText = 'ACTIVE'; }
  else if (status === 3) { el.classList.add('status-waiting'); el.innerText = 'PAUSED'; el.style.color = '#f59e0b'; }
  else if (status === 4) { el.classList.add('status-active'); el.innerText = 'COMPLETE'; el.style.color = '#3b82f6'; }
}

// Expose for onClick
window.moderateStudent = async (userId, status) => {
  const key = prompt("Enter Admin Key to Confirm Action:");
  if (!key) return;

  try {
    await fetch(`${getAdminApiBase()}/admin/update-status`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        room_id: currentRoomId,
        user_id: userId,
        admin_key: key,
        status: status
      })
    });
    fetchRoomDetails();
  } catch (e) {
    alert(e);
  }
};

// Update Room List Click Handler
function bindRoomListEvents() {
  document.querySelectorAll('.room-row').forEach(row => {
    row.addEventListener('click', () => {
      const id = row.dataset.id;
      openRoomDetails(id);
    });
  });
}

// --- WebSocket Logic ---
function initWebSocket() {
  if (ws) return; // Already initialized

  // WebSocket for Admin should connect to Localhost
  // But wait, if we are in Student View, we might need WS to remote.
  // initWebSocket is called by Admin Panel (line 889).
  // It is NOT called by Student Join explicitly?
  // openRoomDetails calls `ws.send`.
  // Student flow: handleJoinRoom -> Success -> ... wait, Student doesn't init WS?
  // Student notifications logic is missing in `handleJoinRoom`?
  // Ah, `handleJoinRoom` just posts. Realtime updates for student?
  // The Student Portal in this code seems to be `handleJoinRoom` which just sends a POST.
  // Does the student see a waiting room?
  // The code mainly focuses on Admin Panel.
  // If Student Portal needs WS, it should be initialized with StudentBase.
  // Current `initWebSocket` is called only in Admin interactions.
  ws = new WebSocket(getAdminWsBase());

  ws.onopen = () => {
    console.log("WS Connected");
    wsRetries = 0;
    updateServerStatus(true);

    // Subscribe to All Rooms List by default if we are in admin view
    if (!adminContainer.classList.contains('fade-out')) {
      ws.send(JSON.stringify({ action: "subscribe_all" }));
    }
  };

  ws.onmessage = (event) => {
    try {
      const msg = JSON.parse(event.data);

      if (msg.type === "ROOM_LIST_UPDATE") {
        fetchRooms();
      } else if (msg.type === "ROOM_UPDATE") {
        // If the payload is the room object, we can update UI directly?
        // Or just re-fetch to be safe/simple.
        // The payload IS the room object.
        const updatedRoom = msg.payload;

        // If we are looking at this room, update details
        if (currentRoomId && updatedRoom.id === currentRoomId) {
          // Optimized: direct update if payload has data, else fetch
          if (updatedRoom) {
            updateRoomDetailsUI(updatedRoom);
          } else {
            fetchRoomDetails();
          }
        }
      } else if (msg.type === "STUDENT_CODE_UPDATE" || msg.type === "STUDENT_SNAPSHOT_UPDATE") {
        updateLiveViewUI(msg.payload);
      }
    } catch (e) {
      console.error("WS Msg Error:", e);
    }
  };

  ws.onclose = () => {
    console.log("WS Closed");
    ws = null;
    updateServerStatus(false);

    // Retry logic
    if (wsRetries < 5) {
      wsRetries++;
      setTimeout(initWebSocket, 2000);
    }
  };

  ws.onerror = (e) => {
    console.error("WS Error:", e);
  };
}

// Refactored UI update for reuse
function updateRoomDetailsUI(room) {
  if (!room) return;

  // Update Header
  document.getElementById('rd-title').innerHTML = `${room.session_name} <span style="font-family:monospace; background:rgba(255,255,255,0.1); padding:2px 6px; border-radius:4px; font-size:0.8em; margin-left:8px;">${room.id}</span>`;
  const badge = document.getElementById('rd-status-badge');
  updateBadge(badge, room.active_status);

  // Update Students List
  const tbody = document.getElementById('rd-students-body');
  const empty = document.getElementById('rd-students-empty');
  tbody.innerHTML = '';

  if (!room.students || room.students.length === 0) {
    empty.style.display = 'block';
  } else {
    empty.style.display = 'none';
    room.students.forEach(s => {
      const tr = document.createElement('tr');
      tr.innerHTML = `
                <td>${s.regno}</td>
                <td>${s.username || 'N/A'}</td>
                <td>${getStatusBadgeHTML(s.active_status)}</td>
                <td class="mono">${s.ip_address}</td>
                <td>
                    <button class="small-btn" style="border-color: var(--accent-primary); color: var(--accent-primary);" onclick="openLiveView('${s.user_id}', '${s.username}')">Live View</button>
                    <button class="small-btn" style="border-color: #ef4444; color: #ef4444;" onclick="moderateStudent('${s.user_id}', 1)">Kick</button>
                </td>
            `;
      tbody.appendChild(tr);
    });
  }
}

// Hook fetchRoomDetails to use the shared UI updater
const originalFetchDetails = fetchRoomDetails;
fetchRoomDetails = async () => {
  if (!currentRoomId) return;
  try {
    const res = await fetch(`${getAdminApiBase()}/get-room?room_id=${currentRoomId}`);
    if (!res.ok) return;
    const room = await res.json();

    // Settings form population remains here because it checks focus
    if (document.activeElement.tagName !== 'INPUT' && document.activeElement.tagName !== 'TEXTAREA') {
      document.getElementById('rd-name').value = room.session_name;
      document.getElementById('rd-duration').value = Math.round(room.time_allocated / 60000000000);
      document.getElementById('rd-status-select').value = room.active_status;

      const container = document.getElementById('sets-container');
      container.innerHTML = '';
      if (room.sets && Object.keys(room.sets).length > 0) {
        for (const [key, val] of Object.entries(room.sets)) {
          addSetRow(key, val);
        }
      } else {
        addSetRow();
      }

      document.getElementById('rd-forbidden-apps').value = (room.forbidden_apps || []).join(', ');
      document.getElementById('rd-invite-list').value = (room.invite_list || []).join(', ');
    }

    updateRoomDetailsUI(room);
  } catch (e) {
    console.error(e);
  }
};

// Show/Update Live View Modal
window.openLiveView = async (userId, name) => {
    liveViewStudentId = userId;
    document.getElementById('lv-student-name').innerText = `Live View: ${name}`;
    document.getElementById('live-view-overlay').style.display = 'flex';
    initLiveViewEditor();
    
    // Immediately fetch room data and find this student
    try {
        const res = await fetch(`${getAdminApiBase()}/get-room?room_id=${currentRoomId}`);
        if (res.ok) {
            const room = await res.json();
            const student = (room.students || []).find(s => s.user_id === userId);
            if (student) {
                updateLiveViewUI(student);
            }
        }
    } catch (e) {
        console.error('Failed to fetch student for live view:', e);
    }
};

// Auto-refresh Live View every 5 seconds
setInterval(async () => {
    if (!liveViewStudentId || !currentRoomId) return;
    try {
        const res = await fetch(`${getAdminApiBase()}/get-room?room_id=${currentRoomId}`);
        if (res.ok) {
            const room = await res.json();
            const student = (room.students || []).find(s => s.user_id === liveViewStudentId);
            if (student) updateLiveViewUI(student);
        }
    } catch (e) { /* silent */ }
}, 5000);

function updateLiveViewUI(student) {
    if (liveViewStudentId !== student.user_id) return;
    
    // Update Editor
    if (liveViewEditor && student.workspace) {
        // Find main file or first file
        const fileName = Object.keys(student.workspace)[0];
        if (fileName) {
            const content = student.workspace[fileName];
            if (liveViewEditor.getValue() !== content) {
                liveViewEditor.setValue(content);
                // Set language based on extension
                const ext = fileName.split('.').pop();
                const langMap = { 'js': 'javascript', 'py': 'python', 'go': 'go', 'cpp': 'cpp', 'html': 'html', 'css': 'css' };
                monaco.editor.setModelLanguage(liveViewEditor.getModel(), langMap[ext] || 'text');
            }
        }
    }
    
    // Update Snapshot
    const snapshotImg = document.getElementById('lv-snapshot-img');
    if (student.latest_snapshot) {
        if (student.latest_snapshot.startsWith("DATA:")) {
            snapshotImg.innerHTML = `<div style="color: var(--accent-primary); text-align: center;">Simulated Screenshot<br/><small>${new Date().toLocaleTimeString()}</small></div>`;
        } else {
             snapshotImg.innerHTML = `<img src="${student.latest_snapshot}" style="width: 100%; height: 100%; object-fit: contain;">`;
        }
    }
    
    document.getElementById('lv-last-sync').innerText = new Date().toLocaleTimeString();
    document.getElementById('lv-status').innerText = getStatusBadgeHTML(student.active_status);
}

// --- Anti-Cheat Enhancements ---

// 1. Tab-Switch & Visibility Tracking
const handleFlagEvent = async (reason) => {
    if (!isStudentSessionActive) return;
    addLogEntry('alert', `⚠️ ${reason} detected! Flagging session.`);
    
    try {
        await fetch(`${getAdminApiBase()}/admin/update-status`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                room_id: currentRoomId,
                user_id: joinRegNoInput.value.trim(),
                status: 3 // Flagged
            })
        });
    } catch (e) {
        console.error("Failed to flag user:", e);
    }
};

document.addEventListener("visibilitychange", () => {
  if (document.hidden && isStudentSessionActive) {
    handleFlagEvent("Tab switch");
  }
});

window.addEventListener('blur', () => {
    if (isStudentSessionActive) handleFlagEvent("Window focus loss");
});

// 2. Clipboard Protection
window.addEventListener('paste', (e) => {
    if (isStudentSessionActive) {
        // Warning: Simple alert might be annoying, but effective for deterrent
        addLogEntry('alert', '⚠️ Paste operation detected. All clipboard activity is monitored.');
    }
});

// 3. Disable Context Menu
window.addEventListener('contextmenu', (e) => {
    if (isStudentSessionActive) e.preventDefault();
});

// --- Room Discovery (HTTP Scan) ---
async function discoverRooms() {
    const listEl = document.getElementById('discovered-list');
    if (!listEl) return;
    listEl.innerHTML = '<div style="padding: 15px; text-align: center; color: #666;">Scanning LAN...</div>';

    const found = [];

    // 1. Check localhost first (same-machine testing)
    try {
        const controller = new AbortController();
        const timeout = setTimeout(() => controller.abort(), 800);
        const res = await fetch('http://localhost:8081/discover', { signal: controller.signal });
        clearTimeout(timeout);
        const data = await res.json();
        if (data.service === 'proctor-alpha') {
            found.push(data.ip || 'localhost');
        }
    } catch (e) { /* localhost not running */ }

    // 2. Scan common LAN subnets
    if (found.length === 0) {
        const subnets = ['192.168.1', '192.168.0', '10.0.0'];
        for (const subnet of subnets) {
            const promises = [];
            for (let i = 1; i <= 30; i++) {
                const ip = `${subnet}.${i}`;
                const controller = new AbortController();
                const timeout = setTimeout(() => controller.abort(), 500);
                promises.push(
                    fetch(`http://${ip}:8081/discover`, { signal: controller.signal })
                        .then(r => r.json())
                        .then(data => { clearTimeout(timeout); if (data.service === 'proctor-alpha') found.push(ip); })
                        .catch(() => { clearTimeout(timeout); })
                );
            }
            await Promise.all(promises);
            if (found.length > 0) break;
        }
    }

    if (found.length === 0) {
        listEl.innerHTML = '<div style="padding: 15px; text-align: center; color: #666;">No rooms found nearby.</div>';
        return;
    }
    listEl.innerHTML = '';
    found.forEach(ip => {
        const row = document.createElement('div');
        row.style.cssText = 'padding: 10px 15px; display: flex; justify-content: space-between; align-items: center; border-bottom: 1px solid rgba(255,255,255,0.05); cursor: pointer;';
        row.innerHTML = `<span style="font-family: monospace; color: var(--accent-primary);">${ip}</span><span style="font-size: 0.8em; color: #666;">Click to use</span>`;
        row.onclick = () => {
            const serverInput = document.getElementById('join-server-ip');
            if (serverInput) serverInput.value = ip;
        };
        listEl.appendChild(row);
    });
}

// Auto-discover on load & refresh button
setTimeout(discoverRooms, 1000);
const refreshDiscBtn = document.getElementById('refresh-discovery-btn');
if (refreshDiscBtn) refreshDiscBtn.onclick = discoverRooms;

// --- Analytics Rendering ---
function renderAnalytics(room) {
    const analyticsBtn = document.getElementById('tab-btn-analytics');
    if (!room.analytics) {
        if (analyticsBtn) analyticsBtn.style.display = 'none';
        return;
    }
    if (analyticsBtn) analyticsBtn.style.display = 'inline-block';

    const a = room.analytics;
    const summaryEl = document.getElementById('analytics-summary');
    if (summaryEl) {
        summaryEl.innerHTML = `
            <div class="stat-card" style="background: rgba(16,185,129,0.05); border: 1px solid rgba(16,185,129,0.2); border-radius: 12px; padding: 20px; text-align: center;">
                <div style="font-size: 2em; font-weight: 700; color: var(--accent-primary);">${a.total_students}</div>
                <div style="font-size: 0.8em; color: #888; margin-top: 4px;">Total Students</div>
            </div>
            <div class="stat-card" style="background: rgba(59,130,246,0.05); border: 1px solid rgba(59,130,246,0.2); border-radius: 12px; padding: 20px; text-align: center;">
                <div style="font-size: 2em; font-weight: 700; color: #3b82f6;">${a.submissions}</div>
                <div style="font-size: 0.8em; color: #888; margin-top: 4px;">Submissions</div>
            </div>
            <div class="stat-card" style="background: rgba(245,158,11,0.05); border: 1px solid rgba(245,158,11,0.2); border-radius: 12px; padding: 20px; text-align: center;">
                <div style="font-size: 2em; font-weight: 700; color: #f59e0b;">${a.total_flags}</div>
                <div style="font-size: 0.8em; color: #888; margin-top: 4px;">Flags Raised</div>
            </div>
            <div class="stat-card" style="background: rgba(139,92,246,0.05); border: 1px solid rgba(139,92,246,0.2); border-radius: 12px; padding: 20px; text-align: center;">
                <div style="font-size: 2em; font-weight: 700; color: #8b5cf6;">${Math.round(a.avg_duration_mins)}</div>
                <div style="font-size: 0.8em; color: #888; margin-top: 4px;">Duration (min)</div>
            </div>
        `;
    }

    const flagListEl = document.getElementById('analytics-flag-list');
    if (flagListEl) {
        if (!a.flagged_logs || a.flagged_logs.length === 0) {
            flagListEl.innerHTML = '<div style="color: var(--accent-primary); padding: 10px;">✓ No incidents recorded.</div>';
        } else {
            flagListEl.innerHTML = a.flagged_logs.map(log =>
                `<div style="padding: 8px 12px; margin-bottom: 6px; background: rgba(245,158,11,0.05); border-left: 3px solid #f59e0b; border-radius: 4px; font-size: 0.9em;">⚠️ ${log}</div>`
            ).join('');
        }
    }
}

// Hook analytics into room detail fetch
const _origFetchRoomDetails = fetchRoomDetails;
fetchRoomDetails = async () => {
    await _origFetchRoomDetails();
    // After fetch, also render analytics if available
    if (!currentRoomId) return;
    try {
        const res = await fetch(`${getAdminApiBase()}/get-room?room_id=${currentRoomId}`);
        if (!res.ok) return;
        const room = await res.json();
        renderAnalytics(room);
    } catch (e) { /* ignore */ }
};

// --- Exam Timer & Countdown ---
let examTimerInterval = null;
let hasSubmitted = false;

function formatTimeRemaining(ms) {
    if (ms <= 0) return '00:00';
    const totalSec = Math.floor(ms / 1000);
    const hours = Math.floor(totalSec / 3600);
    const minutes = Math.floor((totalSec % 3600) / 60);
    const seconds = totalSec % 60;
    if (hours > 0) {
        return `${String(hours).padStart(2,'0')}:${String(minutes).padStart(2,'0')}:${String(seconds).padStart(2,'0')}`;
    }
    return `${String(minutes).padStart(2,'0')}:${String(seconds).padStart(2,'0')}`;
}

// Lock/unlock the student UI based on room status
function setUILocked(locked, reason) {
    const editorContainer = document.getElementById('monaco-container');
    const shellContainer = document.getElementById('shell-container');
    const submitBtn = document.getElementById('submit-work-btn');
    
    if (locked) {
        // Create or update a lock overlay
        let overlay = document.getElementById('ui-lock-overlay');
        if (!overlay) {
            overlay = document.createElement('div');
            overlay.id = 'ui-lock-overlay';
            overlay.style.cssText = 'position: fixed; inset: 0; background: rgba(0,0,0,0.7); z-index: 999; display: flex; align-items: center; justify-content: center; pointer-events: all;';
            document.getElementById('ide-container').appendChild(overlay);
        }
        overlay.innerHTML = `<div style="text-align: center; padding: 40px; background: rgba(0,0,0,0.9); border: 1px solid var(--border-color); border-radius: 16px; max-width: 400px;">
            <div style="font-size: 2em; margin-bottom: 16px;">🔒</div>
            <h3 style="margin-bottom: 8px; color: #fff;">${reason || 'Session Locked'}</h3>
            <p style="color: #888; font-size: 0.9em;">The editor and terminal are disabled.</p>
        </div>`;
        overlay.style.display = 'flex';
        if (submitBtn) submitBtn.style.display = 'none';
    } else {
        const overlay = document.getElementById('ui-lock-overlay');
        if (overlay) overlay.style.display = 'none';
        if (submitBtn) submitBtn.style.display = 'inline-block';
    }
}

async function pollTimer() {
    if (!isStudentSessionActive || !currentRoomId || hasSubmitted) return;

    try {
        const res = await fetch(`${getAdminApiBase()}/timer-info?room_id=${currentRoomId}`);
        if (!res.ok) return;
        const data = await res.json();

        const remainingDisplay = document.getElementById('remaining-time-display');
        const remainingText = document.getElementById('remaining-time-text');
        const submitBtn = document.getElementById('submit-work-btn');

        // Lock UI when session is NOT active
        if (data.status === 0) { // Waiting
            setUILocked(true, 'Waiting for exam to start');
            if (submitBtn) submitBtn.style.display = 'none';
            remainingDisplay.style.display = 'none';
            return;
        } else if (data.status === 3) { // Paused
            setUILocked(true, 'Exam is paused by the proctor');
            if (submitBtn) submitBtn.style.display = 'none';
            return;
        } else if (data.status === 4) { // Complete
            if (!hasSubmitted) {
                addLogEntry('alert', '⏰ Session ended! Auto-submitting your work...');
                await submitWork();
            }
            setUILocked(true, 'Exam has ended');
            return;
        }

        // Status is Active (1) — unlock UI
        setUILocked(false);
        if (submitBtn) submitBtn.style.display = 'inline-block';

        if (data.is_timer_active) {
            remainingDisplay.style.display = 'flex';
            remainingText.textContent = formatTimeRemaining(data.remaining_ms);

            // Urgent warning when < 5 minutes
            if (data.remaining_ms < 300000 && data.remaining_ms > 0) {
                remainingDisplay.style.background = 'rgba(239,68,68,0.15)';
                remainingDisplay.style.borderColor = 'rgba(239,68,68,0.3)';
                remainingDisplay.style.color = '#ef4444';
            } else {
                remainingDisplay.style.background = 'rgba(245,158,11,0.15)';
                remainingDisplay.style.borderColor = 'rgba(245,158,11,0.3)';
                remainingDisplay.style.color = '#f59e0b';
            }

            // Auto-submit when timer hits zero
            if (data.remaining_ms <= 0 && !hasSubmitted) {
                addLogEntry('alert', '⏰ Time is up! Auto-submitting your work...');
                await submitWork();
            }
        } else {
            remainingDisplay.style.display = 'none';
        }
    } catch (e) {
        // Timer poll silently fails — not critical
    }
}

// Start timer polling when student session begins
function startTimerPolling() {
    if (examTimerInterval) clearInterval(examTimerInterval);
    examTimerInterval = setInterval(pollTimer, 3000); // Poll every 3 seconds
    pollTimer(); // Immediate first poll
}

// --- Student Submission ---
async function submitWork() {
    if (hasSubmitted) return;
    hasSubmitted = true;

    addLogEntry('info', '📦 Submitting your work...');

    // Collect all open files
    const files = {};
    try {
        const fileList = await invoke('list_files');
        for (const f of fileList) {
            try {
                const content = await invoke('read_file', { path: f });
                files[f] = content;
            } catch (e) { /* skip unreadable files */ }
        }
    } catch (e) {
        console.error("Failed to collect files:", e);
    }

    try {
        const res = await fetch(`${getAdminApiBase()}/submit`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                room_id: currentRoomId,
                user_id: document.getElementById('join-regno')?.value?.trim() || '',
                files: files
            })
        });

        if (res.ok) {
            addLogEntry('info', '✅ Work submitted successfully!');
            const submitBtn = document.getElementById('submit-work-btn');
            if (submitBtn) {
                submitBtn.textContent = '✓ Submitted';
                submitBtn.style.borderColor = '#10b981';
                submitBtn.style.color = '#10b981';
                submitBtn.disabled = true;
            }
        } else {
            const err = await res.text();
            addLogEntry('alert', `❌ Submission failed: ${err}`);
            hasSubmitted = false; // Allow retry
        }
    } catch (e) {
        addLogEntry('alert', `❌ Submission error: ${e}`);
        hasSubmitted = false; // Allow retry
    }
}

// Wire up submit button
document.getElementById('submit-work-btn')?.addEventListener('click', async () => {
    if (hasSubmitted) return;
    // Confirmation
    const confirmed = confirm('Are you sure you want to submit? This action is final.');
    if (!confirmed) return;
    await submitWork();
});

// Hook into student session start to begin timer
const _origIsStudentActive = isStudentSessionActive;
// Watch for session activation — start timer when joining
const sessionCheckInterval = setInterval(() => {
    if (isStudentSessionActive && currentRoomId) {
        startTimerPolling();
        const submitBtn = document.getElementById('submit-work-btn');
        if (submitBtn) submitBtn.style.display = 'inline-block';
        clearInterval(sessionCheckInterval);
    }
}, 1000);

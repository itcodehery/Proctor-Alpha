import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";

import "@xterm/xterm/css/xterm.css";

// --- Terminal Factory ---
function createTerminal(containerId, ptyId) {
    const term = new Terminal({
        theme: {
            background: '#1e1e1e',
            foreground: '#ffffff',
            cursor: '#5ce080',
        },
        fontSize: 14,
        fontFamily: 'JetBrains Mono',
        cursorBlink: true,
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

    return { term, fitAddon };
}

// --- Initialize Terminals ---
const editor = createTerminal('terminal-container', 'editor');
const shell = createTerminal('shell-container', 'terminal');

// --- Listen for PTY output from Rust ---
listen("pty-output", (event) => {
    const { pty_id, data } = event.payload;
    const bytes = new Uint8Array(data);

    if (pty_id === 'editor') {
        editor.term.write(bytes);
    } else if (pty_id === 'terminal') {
        shell.term.write(bytes);
    }
});

// --- Handle Global Resizes ---
window.addEventListener('resize', () => {
    editor.fitAddon.fit();
    shell.fitAddon.fit();
});

// Focus editor by default
editor.term.focus();

// --- Dialog and State Management ---
const endSessionBtn = document.getElementById('end-session-btn');
const dialogOverlay = document.getElementById('dialog-overlay');
const closeIcon = document.querySelector('.modal-close-icon');
const appContainer = document.getElementById('app');

const activeIndicators = document.querySelectorAll('.active-only');
const pausedIndicators = document.querySelectorAll('.paused-only');

function setSessionState(isPaused) {
    if (isPaused) {
        appContainer.classList.add('paused-mode');
        dialogOverlay.style.display = 'flex';
        activeIndicators.forEach(el => el.style.display = 'none');
        pausedIndicators.forEach(el => el.style.display = 'flex');
    } else {
        appContainer.classList.remove('paused-mode');
        dialogOverlay.style.display = 'none';
        activeIndicators.forEach(el => el.style.display = 'flex');
        pausedIndicators.forEach(el => el.style.display = 'none');
    }
}

endSessionBtn.addEventListener('click', () => setSessionState(true));
closeIcon.addEventListener('click', () => setSessionState(false));

dialogOverlay.addEventListener('click', (e) => {
    if (e.target === dialogOverlay) setSessionState(false);
});

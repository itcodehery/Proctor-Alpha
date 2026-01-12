mod pty_manager;

use std::collections::HashMap;
use std::sync::Mutex;
use tauri::{AppHandle, Manager, State, WindowEvent, Emitter};
use crate::pty_manager::{spawn_pty, PtyInstance};

pub struct AppState {
    pub ptys: HashMap<String, PtyInstance>,
    pub is_session_active: bool,
}

#[tauri::command]
fn write_to_pty(state: State<'_, Mutex<AppState>>, pty_id: String, data: Vec<u8>) {
    let state = state.lock().unwrap();
    if let Some(pty) = state.ptys.get(&pty_id) {
        let mut writer = pty.writer.lock().unwrap();
        let _ = writer.write_all(&data);
        let _ = writer.flush();
    }
}

#[tauri::command]
fn confirm_exit(app_handle: AppHandle, state: State<'_, Mutex<AppState>>, admin_key: String) {
    // Simple mock check for admin key
    if admin_key == "1915" {
        let mut state = state.lock().unwrap();
        state.is_session_active = false;
        app_handle.exit(0);
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .setup(|app| {
            let app_handle = app.handle().clone();
            let mut ptys = HashMap::new();
            
            // Spawn Editor PTY (Vim)
            // Wrap in a shell loop to prevent exit
            ptys.insert("editor".to_string(), spawn_pty(
                app_handle.clone(), 
                "editor".to_string(), 
                "sh", 
                &["-c", "while true; do vim; done"]
            ));
            
            // Spawn Terminal PTY (Shell)
            // Wrap in a shell loop to prevent exit
            ptys.insert("terminal".to_string(), spawn_pty(
                app_handle.clone(), 
                "terminal".to_string(), 
                "sh", 
                &["-c", "while true; do /bin/bash; done"]
            ));
            
            app.manage(Mutex::new(AppState { 
                ptys,
                is_session_active: true 
            }));

            Ok(())
        })
        .on_window_event(|window, event| {
            if let WindowEvent::CloseRequested { api, .. } = event {
                // Prevent closing the app window directly
                api.prevent_close();
                // Optionally emit an event to the frontend to show the "End Session" dialog
                let _ = window.emit("attempted-close", ());
            }
        })
        .invoke_handler(tauri::generate_handler![write_to_pty, confirm_exit])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}

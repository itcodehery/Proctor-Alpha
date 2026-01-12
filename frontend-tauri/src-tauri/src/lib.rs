mod pty_manager;

use std::collections::HashMap;
use std::sync::Mutex;
use tauri::{AppHandle, Manager, State};
use crate::pty_manager::{spawn_pty, PtyInstance};

pub struct AppState {
    pub ptys: HashMap<String, PtyInstance>,
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

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .setup(|app| {
            let app_handle = app.handle().clone();
            let mut ptys = HashMap::new();
            
            // Spawn Editor PTY (Vim)
            ptys.insert("editor".to_string(), spawn_pty(app_handle.clone(), "editor".to_string(), "vim"));
            
            // Spawn Terminal PTY (Shell)
            // On Linux we usually use bash
            ptys.insert("terminal".to_string(), spawn_pty(app_handle.clone(), "terminal".to_string(), "/bin/bash"));
            
            app.manage(Mutex::new(AppState { ptys }));
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![write_to_pty])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}

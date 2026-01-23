mod pty_manager;

use std::collections::HashMap;
use std::sync::Mutex;
use std::path::PathBuf;
use std::fs;
use std::io::{BufRead, BufReader};
use tauri::{AppHandle, Manager, State, WindowEvent, Emitter};
use crate::pty_manager::{spawn_pty, PtyInstance};
use notify::{Watcher, RecursiveMode, EventKind};

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
fn verify_admin_key(state: State<'_, Mutex<AppState>>, admin_key: String) -> bool {
    if admin_key == "1915" {
        let mut state = state.lock().unwrap();
        state.is_session_active = false;
        return true;
    }
    false
}

#[tauri::command]
fn exit_app(app_handle: AppHandle) {
    app_handle.exit(0);
}

#[tauri::command]
fn save_log(log_content: String) -> Result<(), String> {
    let home_dir = std::env::var("HOME").map_err(|_| "Failed to get HOME directory")?;
    let workspace_path = PathBuf::from(home_dir).join(".proctor_workspace");
    let log_path = workspace_path.join("session_log.txt");
    
    fs::write(log_path, log_content).map_err(|e| e.to_string())?;
    Ok(())
}

fn get_last_line(path: &PathBuf) -> Option<String> {
    if let Ok(file) = fs::File::open(path) {
        let reader = BufReader::new(file);
        return reader.lines().last().and_then(|r| r.ok());
    }
    None
}

#[tauri::command]
fn list_files() -> Result<Vec<String>, String> {
    let home_dir = std::env::var("HOME").map_err(|_| "Failed to get HOME directory")?;
    let workspace_path = PathBuf::from(home_dir).join(".proctor_workspace");
    
    let mut files = Vec::new();
    if let Ok(entries) = fs::read_dir(workspace_path) {
        for entry in entries.flatten() {
            if let Ok(meta) = entry.metadata() {
                if meta.is_file() {
                    let name = entry.file_name().to_string_lossy().to_string();
                    // Don't list the session log
                    if name != "session_log.txt" && !name.starts_with('.') {
                        files.push(name);
                    }
                }
            }
        }
    }
    files.sort();
    Ok(files)
}

#[tauri::command]
fn read_file(name: String) -> Result<String, String> {
    let home_dir = std::env::var("HOME").map_err(|_| "Failed to get HOME directory")?;
    let workspace_path = PathBuf::from(home_dir).join(".proctor_workspace");
    let file_path = workspace_path.join(name);
    
    // Security check: ensure file is inside workspace
    if !file_path.starts_with(workspace_path) {
        return Err("Access denied".into());
    }

    fs::read_to_string(file_path).map_err(|e| e.to_string())
}

#[tauri::command]
fn write_file(name: String, content: String) -> Result<(), String> {
    let home_dir = std::env::var("HOME").map_err(|_| "Failed to get HOME directory")?;
    let workspace_path = PathBuf::from(home_dir).join(".proctor_workspace");
    let file_path = workspace_path.join(name);
    
    if !file_path.starts_with(&workspace_path) {
        return Err("Access denied".into());
    }

    fs::write(file_path, content).map_err(|e| e.to_string())
}

#[tauri::command]
fn create_file(name: String) -> Result<(), String> {
    let home_dir = std::env::var("HOME").map_err(|_| "Failed to get HOME directory")?;
    let workspace_path = PathBuf::from(home_dir).join(".proctor_workspace");
    let file_path = workspace_path.join(name);
    
    if !file_path.starts_with(&workspace_path) {
        return Err("Access denied".into());
    }

    if file_path.exists() {
        return Err("File already exists".into());
    }

    fs::write(file_path, "").map_err(|e| e.to_string())
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .setup(|app| {
            let app_handle = app.handle().clone();
            let mut ptys = HashMap::new();
            
            // Setup hidden workspace directory
            let home_dir = std::env::var("HOME").expect("Failed to get HOME directory");
            let workspace_path = PathBuf::from(&home_dir).join(".proctor_workspace");
            let internal_path = PathBuf::from(&home_dir).join(".proctor_internal");
            
            if !workspace_path.exists() {
                fs::create_dir_all(&workspace_path).expect("Failed to create workspace directory");
            }
            if !internal_path.exists() {
                fs::create_dir_all(&internal_path).expect("Failed to create internal directory");
            }
            
            // Clear previous session logs
            let cmd_history_path = internal_path.join(".cmd_history");
            let session_log_path = workspace_path.join("session_log.txt");
            let _ = fs::write(&cmd_history_path, "");
            let _ = fs::write(&session_log_path, "");
            
            let workspace_path_clone = workspace_path.clone();
            let internal_path_clone = internal_path.clone();
            let app_handle_watcher = app_handle.clone();
            
            // Initialize history size to ignore existing content
            let initial_history_size = fs::metadata(internal_path.join(".cmd_history"))
                .map(|m| m.len())
                .unwrap_or(0);

            // Watcher Thread
            std::thread::spawn(move || {
                let (tx, rx) = std::sync::mpsc::channel();
                let mut watcher = notify::recommended_watcher(tx).unwrap();
                let mut last_history_size = initial_history_size;
                
                if let Err(e) = watcher.watch(&workspace_path_clone, RecursiveMode::Recursive) {
                    eprintln!("Watcher error (workspace): {:?}", e);
                }
                if let Err(e) = watcher.watch(&internal_path_clone, RecursiveMode::Recursive) {
                    eprintln!("Watcher error (internal): {:?}", e);
                }

                for res in rx {
                    match res {
                        Ok(event) => {
                            for path in event.paths {
                                let file_name = path.file_name().unwrap_or_default().to_string_lossy();
                                
                                // Ignore vim swap files and the log file itself
                                if file_name.ends_with(".swp") || file_name.ends_with("~") || file_name == "session_log.txt" {
                                    continue;
                                }

                                if path.starts_with(&internal_path_clone) && file_name == ".cmd_history" {
                                    // Command Logged - Only if file grew (prevents double logging from multiple Modify events)
                                    if let Ok(meta) = fs::metadata(&path) {
                                        let current_size = meta.len();
                                        if current_size > last_history_size {
                                            if let Some(cmd) = get_last_line(&path) {
                                                let _ = app_handle_watcher.emit("log-event", serde_json::json!({
                                                    "type": "command",
                                                    "message": cmd
                                                }));
                                            }
                                            last_history_size = current_size;
                                        }
                                    }
                                } else if path.starts_with(&workspace_path_clone) {
                                    // File Change in Workspace
                                    let kind_str = match event.kind {
                                        EventKind::Create(_) => "Created",
                                        EventKind::Modify(_) => "Modified",
                                        EventKind::Remove(_) => "Deleted",
                                        _ => continue,
                                    };
                                    
                                    let _ = app_handle_watcher.emit("log-event", serde_json::json!({
                                        "type": "file",
                                        "message": format!("{} file '{}'", kind_str, file_name)
                                    }));
                                }
                            }
                        },
                        Err(e) => eprintln!("Watch error: {:?}", e),
                    }
                }
            });

            // Removed Editor PTY (Vim Restricted) - Switched to Monaco
            
            // Spawn Terminal PTY (Full Shell) with history logging
            // MacOS: Use zsh with custom ZDOTDIR to force history logging without messing up user config
            // MacOS: Use zsh with custom ZDOTDIR to force history logging without messing up user config
            #[cfg(target_os = "macos")]
            let shell_cmd = {
                let zshrc_path = internal_path.join(".zshrc");
                let history_file = internal_path.join(".cmd_history");
                
                // create custom .zshrc that sources user's config but forces our history settings
                let zshrc_content = format!(
                    r#"
                    # Source user's default config if it exists
                    [[ -f "$HOME/.zshrc" ]] && source "$HOME/.zshrc"
                    
                    # Force Proctor History Settings
                    export HISTFILE="{}"
                    setopt INC_APPEND_HISTORY
                    setopt SHARE_HISTORY
                    "#,
                    history_file.to_string_lossy()
                );
                
                let _ = fs::write(&zshrc_path, zshrc_content);
                
                // For macOS, we construct a command that sets ZDOTDIR and runs zsh
                format!("export ZDOTDIR='{}'; exec /bin/zsh -l", internal_path.to_string_lossy())
            };
            
            #[cfg(not(target_os = "macos"))]
            let shell_cmd = {
                let history_file = internal_path.join(".cmd_history").to_string_lossy().to_string();
                format!(
                     "export HISTFILE='{}'; export PROMPT_COMMAND='history -a'; while true; do /bin/bash; done", 
                     history_file
                )
            };

            ptys.insert("terminal".to_string(), spawn_pty(
                app_handle.clone(), 
                "terminal".to_string(), 
                "sh",
                &["-c", &shell_cmd],
                workspace_path.clone()
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
        .invoke_handler(tauri::generate_handler![write_to_pty, verify_admin_key, exit_app, save_log, list_files, read_file, write_file, create_file])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}

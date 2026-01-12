use std::{
    io::{Read, Write},
    sync::{Arc, Mutex},
    thread,
};
use portable_pty::{CommandBuilder, NativePtySystem, PtySize, PtySystem};
use tauri::{AppHandle, Emitter};

pub struct PtyInstance {
    pub writer: Arc<Mutex<Box<dyn Write + Send>>>,
}

pub fn spawn_pty(app_handle: AppHandle, pty_id: String, command: &str) -> PtyInstance {
    let pty_system = NativePtySystem::default();

    let pair = pty_system
        .openpty(PtySize {
            rows: 24,
            cols: 80,
            pixel_width: 0,
            pixel_height: 0,
        })
        .expect("failed to open pty");

    let cmd = CommandBuilder::new(command);
    let mut child = pair.slave.spawn_command(cmd).expect("failed to spawn command");

    // Close slave to avoid keeping handles open
    drop(pair.slave);

    let reader = pair.master.try_clone_reader().expect("failed to clone reader");
    let writer = pair.master.take_writer().expect("failed to take writer");
    
    let writer = Arc::new(Mutex::new(writer));
    let writer_clone = Arc::clone(&writer);

    let pty_id_clone = pty_id.clone();
    
    // Read thread
    thread::spawn(move || {
        let mut reader = reader;
        let mut buffer = [0u8; 1024];
        loop {
            match reader.read(&mut buffer) {
                Ok(0) => break,
                Ok(n) => {
                    let data = &buffer[..n];
                    // Emit with pty_id so frontend knows which terminal this belongs to
                    let payload = serde_json::json!({
                        "pty_id": pty_id_clone,
                        "data": data
                    });
                    let _ = app_handle.emit("pty-output", payload);
                }
                Err(_) => break,
            }
        }
    });

    // Handle child exit
    thread::spawn(move || {
        let _ = child.wait();
    });

    PtyInstance { writer: writer_clone }
}

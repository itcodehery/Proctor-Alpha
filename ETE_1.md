Architect and implement a compact Go- based application that combines backend functionality with an operational user interface, emphasizing higher-order design and integration skills. The solution should demonstrate a correctly implemented HTTP server with efficient routing and request management, alongside robust security practices using bcrypt for credential handling. Employ concurrency techniques to optimize execution, leveraging go routines for handling asynchronous workflows. Develop a functional UI capable of interacting with backend endpoints, and maintain a well-organized, readable, and modular codebase that reflects strong software engineering principles. 

# ETE Project Todo List

- [x] **Compact Go-based Application**: Backend core built with Go.
- [x] **Operational User Interface**: Tauri-based cross-platform UI.
- [x] **HTTP Server & Routing**: Multiple endpoints for room management, scanning, and syncing.
- [x] **Robust Security (Bcrypt)**: Implemented bcrypt hashing for `AdminKey` storage and verification.
- [x] **Concurrency (Goroutines)**: Implemented in WebSocket Hub (`realtime.go`) and async persistence (`rooms.go`).
- [x] **Functional UI Interaction**: `main.js` handles API calls and real-time state updates.
- [x] **Modular Codebase**: Logic separated into specialized files (`main`, `rooms`, `realtime`).

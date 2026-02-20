# Backend Technical Flow & IP Usage

This document outlines the technical flow of the backend, specifically focusing on how IP addresses are handled for both the Admin and the Students.

## 1. Overview
The backend is a Go-based generic HTTP and WebSocket server that manages Exam Rooms and Student Sessions. It allows an Admin (Host) to create rooms and Students to join them.

## 2. Admin IP Address Usage
The "Admin IP" refers to the local network IP address of the machine running the backend server.

### Where is it used?
-   **File**: `main.go`
-   **Function**: `GetLocalIP()` and `main()`

### How is it used?
1.  **Detection**: When the server starts, `GetLocalIP()` iterates through the machine's network interfaces to find a valid non-loopback IPv4 address.
    ```go
    // main.go
    func GetLocalIP() string {
        addrs, err := net.InterfaceAddrs()
        // ... checks for non-loopback IPv4 ...
        return ipnet.IP.String()
    }
    ```
2.  **Display**: The detected IP is printed to the terminal console.
    ```go
    // main.go
    fmt.Printf("Admin: Share this IP with students: %s\n", ip)
    ```
3.  **Purpose**: The Admin is expected to manually share this IP address with students. Students use this IP to configure their client applications to connect to the exam server (e.g., `http://<Admin_IP>:8080`). The backend *does not* use this IP for authentication or internal logic; it is purely informational for connectivity.

## 3. Student IP Address Usage
The "Student IP" refers to the IP address from which a student connects to the server.

### Where is it used?
-   **File**: `rooms.go`
-   **Handler**: `JoinRoomHandler`
-   **Struct**: `UserSession`

### How is it used?
1.  **Capture**: When a student sends a request to `/join-room`, the server captures their IP address from the HTTP request's remote address.
    ```go
    // rooms.go -> JoinRoomHandler
    newUser.IpAddress = r.RemoteAddr
    ```
2.  **Storage**: This IP is stored in the `UserSession` struct associated with that student.
    ```go
    // rooms.go
    type UserSession struct {
        // ...
        IpAddress    string      `json:"ip_address"`   // Security tracking
        // ...
    }
    ```
3.  **Purpose**: The stored IP is used for **security tracking**. It allows the Admin to see where a student is connecting from, which can be useful for validating that students are on the correct network or detecting suspicious multiple connections from different locations.

## 4. General Technical Flow

### A. Server Initialization (`main.go`)
1.  The server starts on port `8080`.
2.  `GetLocalIP()` determines the host machine's IP.
3.  WebSocket Hub is initialized (`wsHub`).
4.  HTTP Routes are registered (e.g., `/create-room`, `/join-room`, `/ws`).

### B. Room Creation (`rooms.go`)
1.  Admin calls `/create-room` with an `admin_key`.
2.  A new `Room` is created with a unique `RoomID` and stored in memory (`rooms` map).
3.  The room is saved to `rooms.json` for persistence.

### C. Student Joining (`rooms.go`)
1.  Student calls `/join-room` with `room_id`.
2.  Server verifies the room exists.
3.  Server captures `r.RemoteAddr` (Student IP).
4.  Student is added to the `Room.Students` list.
5.  An update is broadcast via WebSockets to notify the Admin.

### D. Realtime Updates (`realtime.go`)
1.  Clients (Admin/Students) connect to `/ws`.
2.  They subscribe to updates (e.g., specific Room ID).
3.  When state changes (e.g., status update, new student), `broadcastUpdate` sends a message to relevant subscribers.

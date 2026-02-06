# Backend-Frontend Integration Analysis

## Overview
The **Proctor** application uses a `Go` backend (acting as the state of truth) and a `Tauri/JavaScript` frontend. The communication happens entirely via **HTTP REST API** calls on `localhost:8080`.

## Integration Status
**Status:** ✅ **Functional & Correct**

The frontend logic in `main.js` is correctly aligned with the backend handlers in `rooms.go`.
- **Admin Actions** (Create, Update, Monitor) are immediately sent to the backend.
- **Student Actions** (Join) verify against the backend state.
- **Real-time Updates** are achieved via **Polling** (every 3-5 seconds) in the frontend.

---

## 1. Admin Space Interaction

The Admin Dashboard acts as the controller. It reads from and writes to the global `rooms` map in the Go backend.

### Key Interactions:
| Action | Frontend Event | Backend Endpoint | Data Flow |
|ort |ort |ort |ort |
| **View Rooms** | `fetchRooms()` | `GET /get-all-rooms` | Backend returns list of all rooms. |
| **Create Room** | Click "Create Room" | `POST /create-room` | Sends `session_name`, `host_id`, `key` → Backend creates ID & saves. |
| **View Details** | Click Room Row | `GET /get-room?room_id=...` | detailed `Room` struct (incl. students) returned. |
| **Live Monitor** | `setInterval(..., 3000)` | `GET /get-room` | Polls every 3s to show new students joining in real-time. |
| **Update Settings** | Click "Save Changes" | `POST /update-room` | Sends updated fields → Backend updates struct & persists to file. |

### Verification of Updates
> "Are changes that the admin does in the frontend correctly being updated?"

**Yes.**
1.  **Frontend**: When you click "Save Changes" in the Room Details view, `main.js` collects the values (`name`, `duration`, `status`, `sets`) and sends a `JSON` payload to `/update-room`.
2.  **Backend**: The `UpdateRoomHandler` in `rooms.go`:
    - Locks the mutex (`mu.Lock`).
    - Finds the room by ID.
    - Updates only the fields provided (using pointer logic to detect changes).
    - Saves the state to `rooms.json`.
3.  **Reflect**: Because the frontend polls `fetchRoomDetails` every 3 seconds, the UI will always display the latest state from the backend (even if updated by another admin).

---

## 2. Student Space Interaction

The Student View acts as a client that registers itself with a specific room.

### Key Interactions:
| Action | Frontend Event | Backend Endpoint | Data Flow |
|ort |ort |ort |ort |
| **Join Session** | Click "Join Session" | `POST /join-room` | Sends `room_id`, `name`, `regno`. |
| **Validation** | Backend Logic | `JoinRoomHandler` | Checks if Room exists & is not closed. Adds user to `Room.Students`. |

---

## 3. Example Workflow

Here is exactly how the data flows during a standard exam session:

### Step 1: Admin Setup
1.  **frontend**: Admin clicks "Create Room". Fills "Physics 101", Host "PROF_A", Key "123".
2.  **backend**: Creates Room `UUID-1`. Sets Status to `Waiting`.
3.  **frontend**: Admin sees "Physics 101" in the list. Admin shares `UUID-1` (Room ID) with students.

### Step 2: Configuration
1.  **frontend**: Admin opens Room Details. Changes duration to "60 mins" and adds "Set A" URL. Clicks Save.
2.  **backend**: Updates Room `UUID-1` with Duration `60m` and Sets.

### Step 3: Student Entry
1.  **frontend (Student)**: Enters Name "Alice", Reg "A1", Room ID `UUID-1`. Clicks Join.
2.  **backend**: 
    - Finds Room `UUID-1`.
    - Appends Alice to `Students` list.
    - Returns Success.
3.  **frontend (Student)**: Navigates to Exam IDE.

### Step 4: Monitoring
1.  **backend**: Has Alice in `Students` list.
2.  **frontend (Admin)**: The 3-second poll hits `/get-room`.
3.  **frontend (Admin)**: UI updates to show "Alice" in the Students table.

## Recommendations for Improvement (Future)
While the current setup works, here are ways to make it more robust:
1.  **WebSockets**: Replace polling (`setInterval`) with WebSockets for true real-time events (instant student appearance without 3s delay).
2.  **Error Feedback**: The backend currently prints errors to console. It should return structured JSON errors (e.g., `{"error_code": "INVALID_KEY"}`) for better UI feedback.
3.  **Security**: The `admin_key` is sent in plain JSON. In a real deployment, this requires HTTPS.

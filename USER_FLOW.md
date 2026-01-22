# Proctor - User Interaction Flow (LAN Architecture)

## Overview
Proctor is designed as a decentralized, offline-first anti-cheat coding environment. It operates on a **Local Area Network (LAN)** model, mimicking a physical "Exam Hall." 

*   **No Internet Required:** The entire session runs on the local network.
*   **The Admin is the Server:** The Proctor (Admin) hosts the backend on their machine.
*   **The Student is the Client:** Students connect directly to the Admin's Local IP address.

---

## 1. Application Launch & Role Selection
Upon opening the application, the user is presented with a Landing Screen asking for their role. This prevents the full IDE interface from loading prematurely.

### Option A: "I am a Proctor (Admin)"
*   **Goal:** Initialize the exam environment and host the server.
*   **Inputs:**
    *   **Session Name:** (e.g., "DSA Final Exam")
    *   **Duration:** (e.g., 60 minutes)
    *   **Admin Key:** A secret password used to authorize sensitive actions later (like ending the exam).
*   **System Action:**
    1.  Spawns the Go Backend Server locally on port `8080`.
    2.  Detects the machine's **Local IP Address** (e.g., `192.168.1.45`).
    3.  Calls `POST /create-room` to generate a unique **Room ID**.

### Option B: "I am a Student"
*   **Goal:** Connect to an existing exam session.
*   **Inputs:**
    *   **Host IP Address:** (Provided verbally or via projector by the Admin).
    *   **Room ID:** (Provided by the Admin).
    *   **Student Name:** (e.g., "John Doe").
    *   **Registration Number:** (e.g., "REG2024-001").
*   **System Action:**
    1.  Validates inputs.
    2.  Calls `POST /join-room` to the Host IP.
    3.  Enters the **Lobby State**.

---

## 2. The Lobby (Pre-Exam Phase)

### Admin View (Dashboard)
Once the server is running, the Admin sees a **Control Dashboard**:
*   **Connection Info:** Prominently displays **Host IP** and **Room ID** for students to copy.
*   **Student List:** Real-time list of connected students.
    *   Shows: Name, RegNo, Connection Status (Online).
*   **Controls:**
    *   **"Start Exam" Button:** Currently disabled until at least one student joins (optional).

### Student View (Waiting Room)
After successfully joining, the Student sees a **Locked Waiting Screen**:
*   **Status Message:** "Connected. Waiting for Proctor to start the session..."
*   **Session Details:** Shows Session Name and allocated duration.
*   **Restrictions:** The Code Editor and Terminal are **hidden/disabled**.
*   **Background:** The app begins polling the server for the `Active` status.

---

## 3. The Exam (Active Phase)

### Action: Admin clicks "Start Exam"
*   The Backend updates the Room Status to `Active`.
*   The Backend calculates `StartTime` and `EndTime`.

### Student View (The IDE)
Upon receiving the `Active` status signal:
1.  **UI Transition:** The Waiting Screen fades out.
2.  **IDE Loads:** The main `index.html` interface (Monaco Editor + Terminal) appears.
3.  **Timer Starts:** The countdown timer in the top nav syncs with the server's `EndTime`.
4.  **Process Shield:** The app begins background scanning for blacklisted apps (Discord, ChatGPT, etc.) and reports violations to the Admin.

### Admin View (Monitoring)
The Admin Dashboard updates to **Live Monitoring Mode**:
*   **Timer:** Shows remaining time.
*   **Student Status Grid:**
    *   **Online:** Green indicator.
    *   **Offline:** Red indicator (if `LastPing` is old).
    *   **Flagged:** Orange/Red warning if the "Process Shield" detects forbidden apps.
*   **Individual Controls:** Clicking a student allows the Admin to:
    *   **Pause Session:** Lock that specific student's screen.
    *   **Flag:** Manually mark them for review.
    *   **Kick:** Remove them from the room.

---

## 4. Admin Management & Modifications
During the exam, the Admin has absolute control:

### Modify User Session
*   **Scenario:** A student disconnects or needs a bathroom break.
*   **Action:** Admin selects the student and changes status to `Paused`.
*   **Result:** Student's screen locks with a "Session Paused" overlay. Admin can set it back to `Active` to resume.

### Stop Session (Global)
*   **Scenario:** Fire alarm or exam completion.
*   **Action:** Admin clicks "End Session for All."
*   **Auth:** Requires the **Admin Key** (set during setup).
*   **Result:** All student clients receive `Complete` status.

---

## 5. Session Conclusion

### Automatic End
*   When the timer reaches `00:00`, the Student App automatically locks.
*   Current code/files are saved locally or pushed to the server (future feature).
*   **UI:** Shows "Exam Ended. Please wait for instructions."

### Manual End (Student)
*   Student can click "Submit / End Session."
*   **Auth:** Requires **Admin Key** (to prevent accidental/rage quitting), or simply confirms submission to server.

### Manual End (Admin)
*   Admin finalizes the room.
*   Server exports a `session_log.json` containing:
    *   Attendance list.
    *   Flagged incidents (Process Shield alerts).
    *   Session duration.
*   Server shuts down.

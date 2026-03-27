package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// PingHandler updates a student's LastPing timestamp and marks them Online.
// POST /student/ping
// Body: { "room_id": "...", "user_id": "..." }
func PingHandler(w http.ResponseWriter, r *http.Request) {
	enableCors(&w)
	if r.Method == "OPTIONS" {
		return
	}
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RoomID string `json:"room_id"`
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	room, exists := rooms[req.RoomID]
	if !exists {
		http.Error(w, "Room not found", http.StatusNotFound)
		return
	}

	found := false
	for i, s := range room.Students {
		if s.UserID == req.UserID {
			room.Students[i].LastPing = time.Now()
			// Revive student to Online if they were marked Offline
			if room.Students[i].ActiveStatus == Offline {
				room.Students[i].ActiveStatus = Online
				broadcastUpdate(req.RoomID, "ROOM_UPDATE", room)
			}
			found = true
			break
		}
	}

	if !found {
		http.Error(w, "Student not found in room", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Ping received",
	})
}

// startOfflineDetector runs a background goroutine that sweeps all rooms every
// sweepInterval and marks students Offline if their LastPing is older than timeout.
func startOfflineDetector(sweepInterval, timeout time.Duration) {
	go func() {
		ticker := time.NewTicker(sweepInterval)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now()
			mu.Lock()
			for _, room := range rooms {
				// Only check active rooms
				if room.ActiveStatus != Active {
					continue
				}
				changed := false
				for i, s := range room.Students {
					if s.ActiveStatus == Online && now.Sub(s.LastPing) > timeout {
						room.Students[i].ActiveStatus = Offline
						fmt.Printf("[offline-detector] Marked student %s (%s) offline in room %s\n",
							s.Username, s.RegNo, room.ID)
						changed = true
					}
				}
				if changed {
					broadcastUpdate(room.ID, "ROOM_UPDATE", room)
				}
			}
			mu.Unlock()
		}
	}()
}

// KickStudentHandler removes a student from a room entirely.
// POST /admin/kick-student
// Body: { "room_id": "...", "admin_key": "...", "user_id": "..." }
func KickStudentHandler(w http.ResponseWriter, r *http.Request) {
	enableCors(&w)
	if r.Method == "OPTIONS" {
		return
	}
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RoomID   string `json:"room_id"`
		AdminKey string `json:"admin_key"`
		UserID   string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	room, exists := rooms[req.RoomID]
	if !exists {
		http.Error(w, "Room not found", http.StatusNotFound)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(room.AdminKey), []byte(req.AdminKey)); err != nil {
		http.Error(w, "Unauthorized: Invalid Admin Key", http.StatusUnauthorized)
		return
	}

	originalLen := len(room.Students)
	filtered := room.Students[:0]
	for _, s := range room.Students {
		if s.UserID != req.UserID {
			filtered = append(filtered, s)
		}
	}

	if len(filtered) == originalLen {
		http.Error(w, "Student not found in room", http.StatusNotFound)
		return
	}

	room.Students = filtered

	broadcastUpdate(req.RoomID, "ROOM_UPDATE", room)
	broadcastUpdate("all", "ROOM_LIST_UPDATE", nil)
	go saveRooms()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Student removed from room",
	})
}

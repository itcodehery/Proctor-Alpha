package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// StatusEnum defines the current state of the Exam Room
type StatusEnum int

const (
	Waiting StatusEnum = iota
	Active
	NetworkLoss
	Paused
	Complete
)

// UStatusEnum defines the state of an individual student
type UStatusEnum int

const (
	Online UStatusEnum = iota
	Offline
	Submitted
	Flagged
)

// Room represents the exam session managed by an examiner
type Room struct {
	ID            string            `json:"id"`
	HostID        string            `json:"host_id"`
	SessionName   string            `json:"session_name"`
	Sets          map[string]string `json:"sets"` // e.g., {"SetA": "Questions_URL_1"}
	ActiveStatus  StatusEnum        `json:"active_status"`
	AdminKey      string            `json:"admin_key"` // Changed to string for better security
	TimeAllocated time.Duration     `json:"time_allocated"`
	StartTime     time.Time         `json:"start_time"`
	EndTime       time.Time         `json:"end_time"`
	Students      []UserSession     `json:"students"`
}

// UserSession represents the student's state within a specific room
type UserSession struct {
	ID           string      `json:"id"`
	UserID       string      `json:"user_id"`
	Username     string      `json:"username"`
	RegNo        string      `json:"regno"`
	ActiveStatus UStatusEnum `json:"active_status"`
	SelectedSet  string      `json:"selected_set"` // Changed to string to match Room.Sets key
	IpAddress    string      `json:"ip_address"`   // Security tracking
	LastPing     time.Time   `json:"last_ping"`    // To detect disconnects
	Score        float64     `json:"score"`        // Optional: for auto-grading
}

var (
	rooms = make(map[string]*Room)
	mu    sync.RWMutex
)

func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// StartExamHandler allows the admin to start the exam
func StartExamHandler(w http.ResponseWriter, r *http.Request) {
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

	if room.AdminKey != req.AdminKey {
		http.Error(w, "Unauthorized: Invalid Admin Key", http.StatusUnauthorized)
		return
	}

	if room.ActiveStatus != Waiting {
		http.Error(w, "Exam can only be started from Waiting state", http.StatusBadRequest)
		return
	}

	room.ActiveStatus = Active
	room.StartTime = time.Now()
	// If TimeAllocated is 0, assume infinite or manual stop?
	// Let's just calculate EndTime if TimeAllocated > 0
	if room.TimeAllocated > 0 {
		room.EndTime = room.StartTime.Add(room.TimeAllocated)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":    "Exam started successfully",
		"start_time": room.StartTime,
		"end_time":   room.EndTime,
	})
}

// CreateRoomHandler handles the creation of a new exam room
func CreateRoomHandler(w http.ResponseWriter, r *http.Request) {
	enableCors(&w)
	if r.Method == "OPTIONS" {
		return
	}
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req Room
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	req.ID = generateID()
	req.Students = []UserSession{}
	if req.ActiveStatus == 0 {
		req.ActiveStatus = Waiting
	}

	mu.Lock()
	rooms[req.ID] = &req
	mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"room_id": req.ID,
		"message": "Room created successfully",
	})
}

// JoinRoomHandler allows a user to join a specific room
func JoinRoomHandler(w http.ResponseWriter, r *http.Request) {
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
		UserSession
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

	// Check if user already exists
	for _, s := range room.Students {
		if s.UserID == req.UserID || (s.RegNo == req.RegNo && req.RegNo != "") {
			// User already in room, maybe return existing session or update?
			// For now, let's just return success with existing ID
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"message":         "User already in room",
				"user_session_id": s.ID,
			})
			return
		}
	}

	newUser := req.UserSession
	newUser.ID = generateID()
	newUser.ActiveStatus = Online
	newUser.LastPing = time.Now()
	newUser.IpAddress = r.RemoteAddr

	room.Students = append(room.Students, newUser)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message":         "Joined successfully",
		"user_session_id": newUser.ID,
	})
}

// AdminUpdateUserHandler allows the admin to modify a user's status
func AdminUpdateUserHandler(w http.ResponseWriter, r *http.Request) {
	enableCors(&w)
	if r.Method == "OPTIONS" {
		return
	}
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RoomID   string      `json:"room_id"`
		AdminKey string      `json:"admin_key"`
		UserID   string      `json:"user_id"`
		Status   UStatusEnum `json:"status"`
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

	if room.AdminKey != req.AdminKey {
		http.Error(w, "Unauthorized: Invalid Admin Key", http.StatusUnauthorized)
		return
	}

	found := false
	for i, s := range room.Students {
		if s.UserID == req.UserID {
			room.Students[i].ActiveStatus = req.Status
			found = true
			break
		}
	}

	if !found {
		http.Error(w, "User not found in room", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "User status updated successfully",
	})
}

// GetRoomHandler allows fetching room details (useful for polling)
func GetRoomHandler(w http.ResponseWriter, r *http.Request) {
	enableCors(&w)
	if r.Method == "OPTIONS" {
		return
	}

	roomID := r.URL.Query().Get("room_id")
	if roomID == "" {
		http.Error(w, "room_id is required", http.StatusBadRequest)
		return
	}

	mu.RLock()
	room, exists := rooms[roomID]
	mu.RUnlock()

	if !exists {
		http.Error(w, "Room not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(room)
}

// GetAllRoomsHandler returns a list of all current rooms (active or waiting)
func GetAllRoomsHandler(w http.ResponseWriter, r *http.Request) {
	enableCors(&w)
	if r.Method == "OPTIONS" {
		return
	}

	mu.RLock()
	defer mu.RUnlock()

	roomList := make([]*Room, 0, len(rooms))
	for _, room := range rooms {
		roomList = append(roomList, room)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(roomList)
}

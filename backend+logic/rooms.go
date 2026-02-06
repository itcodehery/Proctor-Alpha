package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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
	// wsHub is defined in main.go but accessible here as same package
)

func broadcastUpdate(target string, msgType string, payload interface{}) {
	if wsHub == nil {
		return
	}
	wsHub.broadcast <- Message{
		Type:    msgType,
		Payload: payload,
		Target:  target,
	}
}

func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func generateShortRoomID() string {
	b := make([]byte, 6)
	rand.Read(b)
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b)
}

// File path for persistence
const dataFile = "rooms.json"

func init() {
	loadRooms()
}

func loadRooms() {
	file, err := os.Open(dataFile)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		fmt.Println("Error reading rooms.json:", err)
		return
	}
	defer file.Close()

	var loaded map[string]*Room
	if err := json.NewDecoder(file).Decode(&loaded); err != nil {
		fmt.Println("Error decoding rooms.json:", err)
		return
	}

	mu.Lock()
	rooms = loaded
	mu.Unlock()
}

func saveRooms() {
	mu.Lock()
	defer mu.Unlock()

	file, err := os.Create(dataFile)
	if err != nil {
		fmt.Println("Error saving rooms.json:", err)
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(rooms); err != nil {
		fmt.Println("Error encoding rooms.json:", err)
	}
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

	var req struct {
		SessionName string `json:"session_name"`
		HostID      string `json:"host_id"`
		AdminKey    string `json:"admin_key"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Generate a Short ID (6 chars)
	var roomID string
	for {
		roomID = generateShortRoomID()
		mu.Lock()
		_, exists := rooms[roomID]
		mu.Unlock()
		if !exists {
			break
		}
	}

	newRoom := &Room{
		ID:           roomID,
		SessionName:  req.SessionName,
		HostID:       req.HostID,
		AdminKey:     req.AdminKey,
		ActiveStatus: Waiting, // Default status
		Students:     []UserSession{},
		Sets:         make(map[string]string),
	}

	mu.Lock()
	rooms[roomID] = newRoom
	mu.Unlock()

	saveRooms() // Persist the new room
	
	// Broadcast List Update
	broadcastUpdate("all", "ROOM_LIST_UPDATE", nil)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"room_id": roomID,
		"message": "Room created successfully",
	})
}

// JoinRoomHandler allows a user to join a specific room
func JoinRoomHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("[DEBUG] JoinRoomHandler Hit")
	enableCors(&w)
	if r.Method == "OPTIONS" {
		fmt.Println("[DEBUG] JoinRoomHandler OPTIONS")
		return
	}
	if r.Method != "POST" {
		fmt.Println("[DEBUG] JoinRoomHandler Method Not Allowed:", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RoomID string `json:"room_id"`
		UserSession
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fmt.Println("[DEBUG] JoinRoomHandler Decode Error:", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fmt.Printf("[DEBUG] Join Request: %+v\n", req)

	mu.Lock()
	defer mu.Unlock()

	room, exists := rooms[req.RoomID]
	if !exists {
		fmt.Printf("[DEBUG] Room Not Found: %s. Available: %v\n", req.RoomID, rooms)
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

	// Broadcast Room Update (specifically to observers of this room)
	broadcastUpdate(req.RoomID, "ROOM_UPDATE", room)

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
			
			// Broadcast Update
			broadcastUpdate(req.RoomID, "ROOM_UPDATE", room)			
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

// UpdateRoomHandler allows updating room details
func UpdateRoomHandler(w http.ResponseWriter, r *http.Request) {
	enableCors(&w)
	if r.Method == "OPTIONS" {
		return
	}
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RoomID        string            `json:"room_id"`
		AdminKey      string            `json:"admin_key"`
		SessionName   *string           `json:"session_name"`
		Sets          map[string]string `json:"sets"`
		TimeAllocated *time.Duration    `json:"time_allocated"`
		ActiveStatus  *StatusEnum       `json:"active_status"`
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

	// Update fields if provided
	if req.SessionName != nil {
		room.SessionName = *req.SessionName
	}
	if req.Sets != nil {
		room.Sets = req.Sets
	}
	if req.TimeAllocated != nil {
		room.TimeAllocated = *req.TimeAllocated
		// Recalculate end time if active?
		if room.ActiveStatus == Active {
			room.EndTime = room.StartTime.Add(room.TimeAllocated)
		}
	}
	if req.ActiveStatus != nil {
		// Logic changes based on status?
		if *req.ActiveStatus == Active && room.ActiveStatus == Waiting {
			room.StartTime = time.Now()
			if room.TimeAllocated > 0 {
				room.EndTime = room.StartTime.Add(room.TimeAllocated)
			}
		}
		room.ActiveStatus = *req.ActiveStatus
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Room updated successfully",
	})

	// Broadcast updates
	broadcastUpdate(req.RoomID, "ROOM_UPDATE", room)
	// Also broadcast list update in case name/status changed
	broadcastUpdate("all", "ROOM_LIST_UPDATE", nil)

	// Save state
	go saveRooms()
}

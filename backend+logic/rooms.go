package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
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
	ForbiddenApps []string          `json:"forbidden_apps"`
	InviteList    []string          `json:"invite_list"`
	Students      []UserSession     `json:"students"`
	Analytics     *RoomAnalytics    `json:"analytics,omitempty"`
}

type RoomAnalytics struct {
	TotalStudents int            `json:"total_students"`
	TotalFlags    int            `json:"total_flags"`
	AvgDuration   float64        `json:"avg_duration_mins"`
	Submissions   int            `json:"submissions"`
	FlaggedLogs   []string       `json:"flagged_logs"`
}

// UserSession represents the student's state within a specific room
type UserSession struct {
	ID           string      `json:"id"`
	UserID       string      `json:"user_id"`
	Username     string      `json:"username"`
	RegNo        string      `json:"regno"`
	ActiveStatus UStatusEnum `json:"active_status"`
	SelectedSet  string      `json:"selected_set"`
	IpAddress      string            `json:"ip_address"`
	LastPing       time.Time         `json:"last_ping"`
	Score          float64           `json:"score"`
	Workspace      map[string]string `json:"workspace"`
	ActiveFile     string            `json:"active_file"`
	LatestSnapshot string            `json:"latest_snapshot"`
	ActivityLog    []string          `json:"activity_log"`
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
func SyncCodeHandler(w http.ResponseWriter, r *http.Request) {
	enableCors(&w)
	if r.Method == "OPTIONS" {
		return
	}
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RoomID     string            `json:"room_id"`
		UserID     string            `json:"user_id"`
		Files      map[string]string `json:"files"`
		ActiveFile string            `json:"active_file"`
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

	for i, s := range room.Students {
		if s.UserID == req.UserID {
			if room.Students[i].Workspace == nil {
				room.Students[i].Workspace = make(map[string]string)
			}
			for name, content := range req.Files {
				room.Students[i].Workspace[name] = content
			}
			if req.ActiveFile != "" {
				room.Students[i].ActiveFile = req.ActiveFile
			}
			room.Students[i].LastPing = time.Now()
			broadcastUpdate(req.RoomID, "STUDENT_CODE_UPDATE", room.Students[i])
			break
		}
	}

	w.WriteHeader(http.StatusOK)
}

func SnapshotHandler(w http.ResponseWriter, r *http.Request) {
	enableCors(&w)
	if r.Method == "OPTIONS" {
		return
	}
	var req struct {
		RoomID   string `json:"room_id"`
		UserID   string `json:"user_id"`
		Snapshot string `json:"snapshot"`
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
	for i, s := range room.Students {
		if s.UserID == req.UserID {
			room.Students[i].LatestSnapshot = req.Snapshot
			broadcastUpdate(req.RoomID, "STUDENT_SNAPSHOT_UPDATE", room.Students[i])
			break
		}
	}
	w.WriteHeader(http.StatusOK)
}

// StartExamHandler allows the admin to start the exam
// LogActivityHandler records student activity events
func LogActivityHandler(w http.ResponseWriter, r *http.Request) {
	enableCors(&w)
	if r.Method == "OPTIONS" {
		return
	}
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		RoomID  string `json:"room_id"`
		UserID  string `json:"user_id"`
		Message string `json:"message"`
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

	timestamp := time.Now().Format("15:04:05")
	entry := fmt.Sprintf("[%s] %s", timestamp, req.Message)

	for i, s := range room.Students {
		if s.UserID == req.UserID {
			room.Students[i].ActivityLog = append(room.Students[i].ActivityLog, entry)
			if len(room.Students[i].ActivityLog) > 100 {
				room.Students[i].ActivityLog = room.Students[i].ActivityLog[len(room.Students[i].ActivityLog)-100:]
			}
			break
		}
	}
	w.WriteHeader(http.StatusOK)
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

	if err := bcrypt.CompareHashAndPassword([]byte(room.AdminKey), []byte(req.AdminKey)); err != nil {
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
		ID:            roomID,
		SessionName:   req.SessionName,
		HostID:        req.HostID,
		ActiveStatus:  Waiting,
		Students:      []UserSession{},
		Sets:          make(map[string]string),
		ForbiddenApps: []string{},
		InviteList:    []string{},
	}

	// Hash the Admin Key
	hashedKey, err := bcrypt.GenerateFromPassword([]byte(req.AdminKey), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Internal Server Error during security setup", http.StatusInternalServerError)
		return
	}
	newRoom.AdminKey = string(hashedKey)

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

	// Block joining completed or paused rooms
	if room.ActiveStatus == Complete {
		http.Error(w, "This exam session has ended. You cannot join a completed room.", http.StatusForbidden)
		return
	}
	if room.ActiveStatus == Paused {
		http.Error(w, "This exam session is currently paused. Please wait.", http.StatusForbidden)
		return
	}

	// Check if user is in invite list
	if len(room.InviteList) > 0 {
		invited := false
		for _, inv := range room.InviteList {
			if inv == req.RegNo {
				invited = true
				break
			}
		}
		if !invited {
			http.Error(w, "You are not in the invite list for this room", http.StatusForbidden)
			return
		}
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

	if err := bcrypt.CompareHashAndPassword([]byte(room.AdminKey), []byte(req.AdminKey)); err != nil {
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
		ForbiddenApps []string          `json:"forbidden_apps"`
		InviteList    []string          `json:"invite_list"`
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

	if err := bcrypt.CompareHashAndPassword([]byte(room.AdminKey), []byte(req.AdminKey)); err != nil {
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
	if req.ForbiddenApps != nil {
		room.ForbiddenApps = req.ForbiddenApps
	}
	if req.InviteList != nil {
		room.InviteList = req.InviteList
	}
	if req.TimeAllocated != nil {
		room.TimeAllocated = *req.TimeAllocated
		// Recalculate end time if active?
		if room.ActiveStatus == Active {
			room.EndTime = room.StartTime.Add(room.TimeAllocated)
		}
	}
	if req.ActiveStatus != nil {
		if *req.ActiveStatus == Active && room.ActiveStatus == Waiting {
			room.StartTime = time.Now()
			if room.TimeAllocated > 0 {
				room.EndTime = room.StartTime.Add(room.TimeAllocated)
			}
		}
		if *req.ActiveStatus == Complete && room.ActiveStatus != Complete {
			room.EndTime = time.Now()
			// Generate Analytics
			analytics := RoomAnalytics{
				TotalStudents: len(room.Students),
			}
			for _, s := range room.Students {
				if s.ActiveStatus == 3 { // Flagged
					analytics.TotalFlags++
					analytics.FlaggedLogs = append(analytics.FlaggedLogs, fmt.Sprintf("Student %s (%s) was flagged", s.Username, s.RegNo))
				}
				if s.ActiveStatus == 2 { // Submitted
					analytics.Submissions++
				}
			}
			if !room.StartTime.IsZero() {
				analytics.AvgDuration = room.EndTime.Sub(room.StartTime).Minutes()
			}
			room.Analytics = &analytics

			// Export Report
			home, _ := os.UserHomeDir()
			reportPath := filepath.Join(home, ".proctor_workspace", "reports", room.ID+"_report.json")
			os.MkdirAll(filepath.Dir(reportPath), 0755)
			reportData, _ := json.MarshalIndent(room, "", "  ")
			os.WriteFile(reportPath, reportData, 0644)
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

// SubmitHandler allows a student to submit their work
func SubmitHandler(w http.ResponseWriter, r *http.Request) {
	enableCors(&w)
	if r.Method == "OPTIONS" {
		return
	}
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RoomID string            `json:"room_id"`
		UserID string            `json:"user_id"`
		Files  map[string]string `json:"files"` // Final workspace snapshot
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
			room.Students[i].ActiveStatus = Submitted

			// Save final workspace
			if req.Files != nil {
				room.Students[i].Workspace = req.Files
			}

			// Write submission to disk
			home, _ := os.UserHomeDir()
			submissionDir := filepath.Join(home, ".proctor_workspace", "submissions", room.ID, s.RegNo)
			os.MkdirAll(submissionDir, 0755)
			for filename, content := range room.Students[i].Workspace {
				os.WriteFile(filepath.Join(submissionDir, filename), []byte(content), 0644)
			}

			fmt.Printf("Student %s (%s) submitted in room %s\n", s.Username, s.RegNo, room.ID)
			found = true
			break
		}
	}

	if !found {
		http.Error(w, "Student not found in room", http.StatusNotFound)
		return
	}

	broadcastUpdate(req.RoomID, "ROOM_UPDATE", room)
	go saveRooms()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Submission received successfully",
	})
}

// TimerInfoHandler returns the remaining time for a room
func TimerInfoHandler(w http.ResponseWriter, r *http.Request) {
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

	response := struct {
		Status        StatusEnum `json:"status"`
		TimeAllocated int64      `json:"time_allocated_ms"`
		StartTime     int64      `json:"start_time_ms"`
		EndTime       int64      `json:"end_time_ms"`
		RemainingMs   int64      `json:"remaining_ms"`
		IsTimerActive bool       `json:"is_timer_active"`
	}{
		Status:        room.ActiveStatus,
		TimeAllocated: room.TimeAllocated.Milliseconds(),
		StartTime:     room.StartTime.UnixMilli(),
		IsTimerActive: room.ActiveStatus == Active && room.TimeAllocated > 0,
	}

	if room.ActiveStatus == Active && room.TimeAllocated > 0 {
		endTime := room.StartTime.Add(room.TimeAllocated)
		response.EndTime = endTime.UnixMilli()
		remaining := time.Until(endTime)
		if remaining < 0 {
			remaining = 0
		}
		response.RemainingMs = remaining.Milliseconds()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

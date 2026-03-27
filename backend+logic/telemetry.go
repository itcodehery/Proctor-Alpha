package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// TelemetryEvent represents a raw event from a student client.
type TelemetryEvent struct {
	RoomID    string
	UserID    string
	EventType string // "focus_lost", "process_scan", "tab_switch"
	RawData   json.RawMessage
	Timestamp time.Time
}

// ProcessScanData holds the list of processes sent from the client.
type ProcessScanData struct {
	Processes []string `json:"processes"`
}

// FocusData holds information about window blur events.
type FocusData struct {
	DurationSeconds float64 `json:"duration_seconds"`
}

// TelemetryQueue is the buffered channel for our worker pool.
var TelemetryQueue = make(chan TelemetryEvent, 1000)

// StartTelemetryWorkers initializes the worker pool to process events asynchronously.
func StartTelemetryWorkers(numWorkers int) {
	for i := 1; i <= numWorkers; i++ {
		go telemetryWorker(i, TelemetryQueue)
	}
	fmt.Printf("Started %d telemetry workers.\n", numWorkers)
}

func telemetryWorker(id int, queue <-chan TelemetryEvent) {
	for event := range queue {
		processTelemetryEvent(event)
	}
}

func processTelemetryEvent(event TelemetryEvent) {
	mu.Lock()
	room, exists := rooms[event.RoomID]
	if !exists {
		mu.Unlock()
		return
	}

	var targetStudent *UserSession
	studentIndex := -1
	for i := range room.Students {
		if room.Students[i].UserID == event.UserID {
			targetStudent = &room.Students[i]
			studentIndex = i
			break
		}
	}

	if targetStudent == nil {
		mu.Unlock()
		return
	}

	// Calculate mathematical suspicion score increment
	var scoreIncrement float64
	var reasoning string

	switch event.EventType {
	case "focus_lost":
		var fd FocusData
		if err := json.Unmarshal(event.RawData, &fd); err == nil {
			// MATH: Base weight 5, multiplier grows exponentially with duration (e.g., math.Pow)
			// S_score = base + 1.5 ^ (duration/10)
			scoreIncrement = 5.0 + math.Pow(1.5, fd.DurationSeconds/10.0)
			scoreIncrement = math.Min(scoreIncrement, 50.0) // Cap the score per event
			reasoning = fmt.Sprintf("Focus lost for %.1fs", fd.DurationSeconds)
		}

	case "process_scan":
		var pd ProcessScanData
		if err := json.Unmarshal(event.RawData, &pd); err == nil {
			// Evaluate processes using Advanced String Manipulation
			matches := evaluateProcesses(pd.Processes, room.ForbiddenApps)
			if len(matches) > 0 {
				scoreIncrement = float64(len(matches) * 20) // 20 points per forbidden app found
				reasoning = fmt.Sprintf("Forbidden processes detected: %s", strings.Join(matches, ", "))
			}
		}

	case "tab_switch":
		scoreIncrement = 10.0
		reasoning = "Student switched browser tabs or workspaces"
	}

	// Apply score and math logic for sliding window thresholding
	if scoreIncrement > 0 {
		newTotal := targetStudent.TotalSuspicionScore + scoreIncrement
		
		fmt.Printf("[Telemetry Worker] User %s gained %.1f suspicion points for %s. Total: %.1f\n", 
			targetStudent.Username, scoreIncrement, reasoning, newTotal)

		entry := fmt.Sprintf("[%s] ⚠️ %s (Score +%.1f)", event.Timestamp.Format("15:04:05"), reasoning, scoreIncrement)
		
		targetStudent.ActivityLog = append(targetStudent.ActivityLog, entry)
		if len(targetStudent.ActivityLog) > 100 {
			targetStudent.ActivityLog = targetStudent.ActivityLog[len(targetStudent.ActivityLog)-100:]
		}
		
		targetStudent.TotalSuspicionScore = newTotal

		// MATH: Threshold check
		// If score > 100, auto-flag the student
		if newTotal >= 100.0 && targetStudent.ActiveStatus != Flagged {
			targetStudent.ActiveStatus = Flagged
			broadcastUpdate(room.ID, "ROOM_UPDATE", room)
			
			// Append critical flag log
			flagEntry := fmt.Sprintf("[%s] 🚨 AUTO-FLAGGED: Suspicion score exceeded threshold.", time.Now().Format("15:04:05"))
			targetStudent.ActivityLog = append(targetStudent.ActivityLog, flagEntry)
		} else {
			// Update just the student model if not auto-flagged, to refresh logs
			broadcastUpdate(room.ID, "STUDENT_CODE_UPDATE", targetStudent)
		}
		
		// Update the room struct
		room.Students[studentIndex] = *targetStudent
	}
	
	// Unlock BEFORE calling saveRooms to prevent deadlock
	// (saveRooms also acquires mu.Lock)
	mu.Unlock()
	
	if scoreIncrement > 0 {
		go saveRooms()
	}
}

// evaluateProcesses uses advanced string manipulation and regex 
// to loosely match running processes with forbidden apps.
func evaluateProcesses(running []string, forbidden []string) []string {
	if len(forbidden) == 0 {
		return nil
	}

	var found []string
	for _, process := range running {
		procLower := strings.ToLower(strings.TrimSpace(process))
		
		// Clean up the process name (remove extension, remove path)
		// e.g., "C:\Program Files\Discord\discord.exe" -> "discord"
		parts := strings.Split(procLower, "/")
		if len(parts) == 1 {
			parts = strings.Split(procLower, "\\")
		}
		executable := parts[len(parts)-1]
		executable = strings.ReplaceAll(executable, ".exe", "")
		executable = strings.ReplaceAll(executable, ".app", "")

		for _, banned := range forbidden {
			bannedLower := strings.ToLower(strings.TrimSpace(banned))
			
			// 1. Exact or Substring match
			if strings.Contains(executable, bannedLower) {
				found = append(found, bannedLower)
				continue
			}

			// 2. Regex matching (e.g. matching 'discord-ptb' or 'slack_helper')
			pattern := fmt.Sprintf(`.*%s.*`, regexp.QuoteMeta(bannedLower))
			matched, _ := regexp.MatchString(pattern, executable)
			if matched {
				found = append(found, bannedLower)
				continue
			}
			
			// 3. Mathematical Levenshtein Distance (Fuzzy Match for slight variations/typos)
			// Calculate similarity between 'executable' and 'bannedLower'
			dist := levenshteinDistance(executable, bannedLower)
			
			// If strings are similar (e.g. only 1 or 2 chars difference depending on length)
			// This prevents spoofing like renaming discord.exe to d1scord.exe
			maxAllowedDist := int(math.Max(1.0, float64(len(bannedLower))/4.0)) 
			if dist <= maxAllowedDist && len(bannedLower) > 3 {
				found = append(found, fmt.Sprintf("%s (fuzzy match: %s)", bannedLower, executable))
			}
		}
	}
	
	return deduplicate(found)
}

// levenshteinDistance is a mathematical algorithm computing the minimum number of
// single-character edits required to change word1 into word2.
func levenshteinDistance(s1, s2 string) int {
	lenS1 := len(s1)
	lenS2 := len(s2)
	
	if lenS1 == 0 { return lenS2 }
	if lenS2 == 0 { return lenS1 }

	matrix := make([][]int, lenS1+1)
	for i := range matrix {
		matrix[i] = make([]int, lenS2+1)
		matrix[i][0] = i
	}
	for j := 0; j <= lenS2; j++ {
		matrix[0][j] = j
	}

	for i := 1; i <= lenS1; i++ {
		for j := 1; j <= lenS2; j++ {
			cost := 1
			if s1[i-1] == s2[j-1] {
				cost = 0
			}
			matrix[i][j] = int(math.Min(
				float64(matrix[i-1][j]+1),           // Deletion
				math.Min(
					float64(matrix[i][j-1]+1),       // Insertion
					float64(matrix[i-1][j-1]+cost),  // Substitution
				),
			))
		}
	}

	return matrix[lenS1][lenS2]
}

func deduplicate(input []string) []string {
	keys := make(map[string]bool)
	list := []string{}
	for _, entry := range input {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}

// TelemetryEndpointHandler receives JSON events and pipes them to the worker queue
func TelemetryEndpointHandler(w http.ResponseWriter, r *http.Request) {
	enableCors(&w)
	if r.Method == "OPTIONS" {
		return
	}
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RoomID    string          `json:"room_id"`
		UserID    string          `json:"user_id"`
		EventType string          `json:"event_type"`
		Data      json.RawMessage `json:"data"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid Payload", http.StatusBadRequest)
		return
	}

	event := TelemetryEvent{
		RoomID:    req.RoomID,
		UserID:    req.UserID,
		EventType: req.EventType,
		RawData:   req.Data,
		Timestamp: time.Now(),
	}

	// Non-blocking send to the worker pool
	select {
	case TelemetryQueue <- event:
		// Successfully queued
	default:
		// Queue full - fall back or drop
		http.Error(w, "Server overloaded, telemetry dropped", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
}

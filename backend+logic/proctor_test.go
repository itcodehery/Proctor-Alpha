package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// =============================================================================
// SECTION 1: UNIT TESTS - Levenshtein Distance (Mathematical Algorithm)
// =============================================================================

func TestLevenshteinDistance_IdenticalStrings(t *testing.T) {
	dist := levenshteinDistance("discord", "discord")
	if dist != 0 {
		t.Errorf("Expected distance 0 for identical strings, got %d", dist)
	}
	t.Logf("levenshtein(\"discord\", \"discord\") = %d [identical strings]", dist)
}

func TestLevenshteinDistance_SingleCharDifference(t *testing.T) {
	dist := levenshteinDistance("discord", "discors")
	if dist != 1 {
		t.Errorf("Expected distance 1, got %d", dist)
	}
	t.Logf("levenshtein(\"discord\", \"discors\") = %d [one substitution]", dist)
}

func TestLevenshteinDistance_SpoofedName(t *testing.T) {
	dist := levenshteinDistance("d1sc0rd", "discord")
	if dist != 2 {
		t.Errorf("Expected distance 2 for spoofed name, got %d", dist)
	}
	t.Logf("levenshtein(\"d1sc0rd\", \"discord\") = %d [spoofed app name]", dist)
}

func TestLevenshteinDistance_CompletelyDifferent(t *testing.T) {
	dist := levenshteinDistance("abc", "xyz")
	if dist != 3 {
		t.Errorf("Expected distance 3 for completely different strings, got %d", dist)
	}
	t.Logf("levenshtein(\"abc\", \"xyz\") = %d [completely different]", dist)
}

func TestLevenshteinDistance_EmptyStrings(t *testing.T) {
	dist1 := levenshteinDistance("", "hello")
	dist2 := levenshteinDistance("hello", "")
	dist3 := levenshteinDistance("", "")
	if dist1 != 5 {
		t.Errorf("Expected 5 for empty vs 'hello', got %d", dist1)
	}
	if dist2 != 5 {
		t.Errorf("Expected 5 for 'hello' vs empty, got %d", dist2)
	}
	if dist3 != 0 {
		t.Errorf("Expected 0 for two empty strings, got %d", dist3)
	}
	t.Logf("Edge cases: (\"\",\"hello\")=%d, (\"hello\",\"\")=%d, (\"\",\"\")=%d", dist1, dist2, dist3)
}

// =============================================================================
// SECTION 2: UNIT TESTS - Process Evaluation (Advanced String Manipulation)
// =============================================================================

func TestEvaluateProcesses_ExactMatch(t *testing.T) {
	running := []string{"chrome", "discord", "code"}
	forbidden := []string{"discord"}
	matches := evaluateProcesses(running, forbidden)
	if len(matches) != 1 || matches[0] != "discord" {
		t.Errorf("Expected [discord], got %v", matches)
	}
	t.Logf("Exact match: running=%v, forbidden=%v -> found=%v", running, forbidden, matches)
}

func TestEvaluateProcesses_PathStripping(t *testing.T) {
	running := []string{"C:\\Program Files\\Discord\\discord.exe"}
	forbidden := []string{"discord"}
	matches := evaluateProcesses(running, forbidden)
	if len(matches) == 0 {
		t.Errorf("Expected to find discord after path stripping, got %v", matches)
	}
	t.Logf("Path stripping: \"%s\" -> detected: %v", running[0], matches)
}

func TestEvaluateProcesses_FuzzyMatch(t *testing.T) {
	running := []string{"d1scord"}
	forbidden := []string{"discord"}
	matches := evaluateProcesses(running, forbidden)
	if len(matches) == 0 {
		t.Errorf("Expected fuzzy match for d1scord ~ discord, got %v", matches)
	}
	t.Logf("Fuzzy match (Levenshtein): \"d1scord\" ~ \"discord\" -> detected: %v", matches)
}

func TestEvaluateProcesses_NoFalsePositives(t *testing.T) {
	running := []string{"chrome", "vscode", "terminal", "finder"}
	forbidden := []string{"discord", "slack", "firefox"}
	matches := evaluateProcesses(running, forbidden)
	if len(matches) != 0 {
		t.Errorf("Expected no matches for safe processes, got %v", matches)
	}
	t.Logf("No false positives: safe processes %v passed cleanly", running)
}

func TestEvaluateProcesses_EmptyForbiddenList(t *testing.T) {
	running := []string{"discord", "slack"}
	forbidden := []string{}
	matches := evaluateProcesses(running, forbidden)
	if matches != nil {
		t.Errorf("Expected nil for empty forbidden list, got %v", matches)
	}
	t.Logf("Empty forbidden list returns nil correctly")
}

func TestEvaluateProcesses_SubstringMatch(t *testing.T) {
	running := []string{"discord-ptb"}
	forbidden := []string{"discord"}
	matches := evaluateProcesses(running, forbidden)
	if len(matches) == 0 {
		t.Errorf("Expected substring match for discord-ptb, got %v", matches)
	}
	t.Logf("Substring match: \"discord-ptb\" contains \"discord\" -> detected: %v", matches)
}

// =============================================================================
// SECTION 3: INTEGRATION TEST - Full Application Lifecycle Simulation
// =============================================================================

// setupTestServer registers all route handlers onto a fresh ServeMux
// and returns an httptest.Server for isolated testing.
func setupTestServer() *httptest.Server {
	mu.Lock()
	rooms = make(map[string]*Room)
	mu.Unlock()

	wsHub = newHub()
	go wsHub.run()

	StartTelemetryWorkers(3)

	mux := http.NewServeMux()
	mux.HandleFunc("/create-room", CreateRoomHandler)
	mux.HandleFunc("/join-room", JoinRoomHandler)
	mux.HandleFunc("/start-exam", StartExamHandler)
	mux.HandleFunc("/sync-code", SyncCodeHandler)
	mux.HandleFunc("/submit", SubmitHandler)
	mux.HandleFunc("/get-room", GetRoomHandler)
	mux.HandleFunc("/get-all-rooms", GetAllRoomsHandler)
	mux.HandleFunc("/log-activity", LogActivityHandler)
	mux.HandleFunc("/telemetry", TelemetryEndpointHandler)
	mux.HandleFunc("/admin/verify-key", VerifyAdminKeyHandler)
	mux.HandleFunc("/admin/update-status", AdminUpdateUserHandler)
	mux.HandleFunc("/update-room", UpdateRoomHandler)

	return httptest.NewServer(mux)
}

func postJSON(url string, payload interface{}) (*http.Response, map[string]interface{}, error) {
	body, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return resp, result, nil
}

func TestFullExamLifecycle(t *testing.T) {
	server := setupTestServer()
	defer server.Close()
	base := server.URL

	adminKey := "mysecretkey123"
	var roomID string
	studentUserID := "REG2024001"

	// STEP 1: Admin creates an exam room
	t.Run("Step01_CreateRoom", func(t *testing.T) {
		resp, data, err := postJSON(base+"/create-room", map[string]string{
			"session_name": "Go Programming Mid-Term",
			"host_id":      "PROF001",
			"admin_key":    adminKey,
		})
		if err != nil {
			t.Fatalf("Failed to create room: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}
		roomID = data["room_id"].(string)
		t.Logf("Room created. ID: %s", roomID)
	})

	// STEP 2: Admin configures room with forbidden apps
	t.Run("Step02_ConfigureRoom", func(t *testing.T) {
		resp, _, err := postJSON(base+"/update-room", map[string]interface{}{
			"room_id":        roomID,
			"admin_key":      adminKey,
			"forbidden_apps": []string{"discord", "slack", "firefox"},
		})
		if err != nil {
			t.Fatalf("Failed to configure room: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}
		t.Logf("Room configured with forbidden apps: [discord, slack, firefox]")
	})

	// STEP 3: Student joins the room
	t.Run("Step03_StudentJoins", func(t *testing.T) {
		resp, data, err := postJSON(base+"/join-room", map[string]string{
			"room_id":  roomID,
			"username": "Praneeth M",
			"regno":    studentUserID,
			"user_id":  studentUserID,
		})
		if err != nil {
			t.Fatalf("Failed to join room: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}
		t.Logf("Student joined. Session ID: %v", data["user_session_id"])
	})

	// STEP 4: Admin key verification (correct + incorrect)
	t.Run("Step04_VerifyAdminKey", func(t *testing.T) {
		resp, _, _ := postJSON(base+"/admin/verify-key", map[string]string{
			"room_id":   roomID,
			"admin_key": adminKey,
		})
		if resp.StatusCode != 200 {
			t.Fatalf("Valid key rejected, status: %d", resp.StatusCode)
		}
		t.Logf("Valid admin key accepted")

		resp2, _, _ := postJSON(base+"/admin/verify-key", map[string]string{
			"room_id":   roomID,
			"admin_key": "wrongkey",
		})
		if resp2.StatusCode != 401 {
			t.Fatalf("Invalid key should return 401, got %d", resp2.StatusCode)
		}
		t.Logf("Invalid admin key correctly rejected (401)")
	})

	// STEP 5: Admin starts the exam
	t.Run("Step05_StartExam", func(t *testing.T) {
		resp, _, err := postJSON(base+"/start-exam", map[string]string{
			"room_id":   roomID,
			"admin_key": adminKey,
		})
		if err != nil {
			t.Fatalf("Failed to start exam: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}
		t.Logf("Exam started successfully")
	})

	// STEP 6: Student syncs code to the server
	t.Run("Step06_SyncCode", func(t *testing.T) {
		resp, _, err := postJSON(base+"/sync-code", map[string]interface{}{
			"room_id": roomID,
			"user_id": studentUserID,
			"files": map[string]string{
				"main.go":  "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello, World!\")\n}",
				"utils.go": "package main\n\nfunc add(a, b int) int {\n\treturn a + b\n}",
			},
			"active_file": "main.go",
		})
		if err != nil {
			t.Fatalf("Failed to sync code: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}
		t.Logf("Code synced: main.go, utils.go (active: main.go)")
	})

	// STEP 7: Verify room state via GET
	t.Run("Step07_VerifyRoomState", func(t *testing.T) {
		resp, err := http.Get(base + "/get-room?room_id=" + roomID)
		if err != nil {
			t.Fatalf("Failed to get room: %v", err)
		}
		defer resp.Body.Close()
		var room Room
		json.NewDecoder(resp.Body).Decode(&room)

		if room.ActiveStatus != Active {
			t.Fatalf("Room should be Active, got %d", room.ActiveStatus)
		}
		if len(room.Students) != 1 {
			t.Fatalf("Expected 1 student, got %d", len(room.Students))
		}
		student := room.Students[0]
		if student.ActiveFile != "main.go" {
			t.Fatalf("Expected active file 'main.go', got '%s'", student.ActiveFile)
		}
		if len(student.Workspace) != 2 {
			t.Fatalf("Expected 2 files in workspace, got %d", len(student.Workspace))
		}
		t.Logf("Room state verified: Status=Active, Students=1, Files=%d, ActiveFile=%s",
			len(student.Workspace), student.ActiveFile)
	})

	// STEP 8: Telemetry - simulate focus loss events (suspicion engine)
	t.Run("Step08_TelemetryFocusLoss", func(t *testing.T) {
		durations := []float64{5.0, 15.0, 30.0}
		for i, dur := range durations {
			data, _ := json.Marshal(map[string]float64{"duration_seconds": dur})
			resp, _, err := postJSON(base+"/telemetry", map[string]interface{}{
				"room_id":    roomID,
				"user_id":    studentUserID,
				"event_type": "focus_lost",
				"data":       json.RawMessage(data),
			})
			if err != nil {
				t.Fatalf("Telemetry event %d failed: %v", i+1, err)
			}
			if resp.StatusCode != 200 {
				t.Fatalf("Expected 200, got %d", resp.StatusCode)
			}
		}

		time.Sleep(500 * time.Millisecond)

		resp, err := http.Get(base + "/get-room?room_id=" + roomID)
		if err != nil {
			t.Fatalf("Failed to get room: %v", err)
		}
		defer resp.Body.Close()
		var room Room
		json.NewDecoder(resp.Body).Decode(&room)

		score := room.Students[0].TotalSuspicionScore
		if score <= 0 {
			t.Fatalf("Suspicion score should be > 0 after focus loss events, got %.1f", score)
		}
		t.Logf("Suspicion score after 3 focus-loss events (5s, 15s, 30s): %.1f", score)
	})

	// STEP 9: Telemetry - simulate forbidden process detection
	t.Run("Step09_TelemetryProcessScan", func(t *testing.T) {
		processes := []string{"chrome", "vscode", "Discord.app", "terminal"}
		data, _ := json.Marshal(map[string][]string{"processes": processes})
		resp, _, err := postJSON(base+"/telemetry", map[string]interface{}{
			"room_id":    roomID,
			"user_id":    studentUserID,
			"event_type": "process_scan",
			"data":       json.RawMessage(data),
		})
		if err != nil {
			t.Fatalf("Process scan telemetry failed: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		time.Sleep(500 * time.Millisecond)

		resp2, err := http.Get(base + "/get-room?room_id=" + roomID)
		if err != nil {
			t.Fatalf("Failed to get room: %v", err)
		}
		defer resp2.Body.Close()
		var room Room
		json.NewDecoder(resp2.Body).Decode(&room)

		t.Logf("After process scan %v: score=%.1f", processes, room.Students[0].TotalSuspicionScore)
	})

	// STEP 10: Telemetry - simulate spoofed process (fuzzy match test)
	t.Run("Step10_TelemetrySpoofedProcess", func(t *testing.T) {
		processes := []string{"chrome", "d1scord", "terminal"}
		data, _ := json.Marshal(map[string][]string{"processes": processes})
		resp, _, err := postJSON(base+"/telemetry", map[string]interface{}{
			"room_id":    roomID,
			"user_id":    studentUserID,
			"event_type": "process_scan",
			"data":       json.RawMessage(data),
		})
		if err != nil {
			t.Fatalf("Spoofed process scan failed: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}

		time.Sleep(500 * time.Millisecond)

		resp2, err := http.Get(base + "/get-room?room_id=" + roomID)
		if err != nil {
			t.Fatalf("Failed to get room: %v", err)
		}
		defer resp2.Body.Close()
		var room Room
		json.NewDecoder(resp2.Body).Decode(&room)

		t.Logf("Spoofed process \"d1scord\" fuzzy-matched to \"discord\": score=%.1f", room.Students[0].TotalSuspicionScore)
	})

	// STEP 11: Verify auto-flagging when threshold is exceeded
	t.Run("Step11_VerifyAutoFlag", func(t *testing.T) {
		resp, err := http.Get(base + "/get-room?room_id=" + roomID)
		if err != nil {
			t.Fatalf("Failed to get room: %v", err)
		}
		defer resp.Body.Close()
		var room Room
		json.NewDecoder(resp.Body).Decode(&room)

		student := room.Students[0]

		if student.TotalSuspicionScore < 100.0 {
			t.Logf("Score %.1f not yet at threshold, sending additional events...", student.TotalSuspicionScore)
			for i := 0; i < 5; i++ {
				data, _ := json.Marshal(map[string]float64{"duration_seconds": 60.0})
				postJSON(base+"/telemetry", map[string]interface{}{
					"room_id":    roomID,
					"user_id":    studentUserID,
					"event_type": "focus_lost",
					"data":       json.RawMessage(data),
				})
				time.Sleep(200 * time.Millisecond)
			}
			time.Sleep(2 * time.Second)

			resp2, err := http.Get(base + "/get-room?room_id=" + roomID)
			if err != nil {
				t.Fatalf("Failed to get room: %v", err)
			}
			defer resp2.Body.Close()
			json.NewDecoder(resp2.Body).Decode(&room)
			student = room.Students[0]
		}

		if student.TotalSuspicionScore < 100.0 {
			t.Fatalf("Score should be >= 100 after sustained suspicious activity, got %.1f", student.TotalSuspicionScore)
		}
		if student.ActiveStatus != Flagged {
			t.Fatalf("Student should be auto-flagged (status=3), got %d", student.ActiveStatus)
		}
		t.Logf("AUTO-FLAG VERIFIED: score=%.1f, status=FLAGGED", student.TotalSuspicionScore)
	})

	// STEP 12: Student submits work
	t.Run("Step12_SubmitWork", func(t *testing.T) {
		resp, data, err := postJSON(base+"/submit", map[string]interface{}{
			"room_id": roomID,
			"user_id": studentUserID,
			"files": map[string]string{
				"main.go":  "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Final submission!\")\n}",
				"utils.go": "package main\n\nfunc add(a, b int) int {\n\treturn a + b\n}\n\nfunc multiply(a, b int) int {\n\treturn a * b\n}",
			},
		})
		if err != nil {
			t.Fatalf("Submission failed: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}
		t.Logf("Student work submitted: %v", data["message"])
	})

	// STEP 13: Final room state summary
	t.Run("Step13_FinalSummary", func(t *testing.T) {
		resp, err := http.Get(base + "/get-room?room_id=" + roomID)
		if err != nil {
			t.Fatalf("Failed to get room: %v", err)
		}
		defer resp.Body.Close()
		var room Room
		json.NewDecoder(resp.Body).Decode(&room)

		student := room.Students[0]

		t.Logf("========== PROCTOR-ALPHA TEST SUMMARY ==========")
		t.Logf("  Room ID:           %s", roomID)
		t.Logf("  Session:           %s", room.SessionName)
		t.Logf("  Room Status:       %d (1=Active)", room.ActiveStatus)
		t.Logf("  Forbidden Apps:    %v", room.ForbiddenApps)
		t.Logf("  Student:           %s (%s)", student.Username, student.RegNo)
		t.Logf("  Student Status:    %d (2=Submitted, 3=Flagged)", student.ActiveStatus)
		t.Logf("  Suspicion Score:   %.1f", student.TotalSuspicionScore)
		t.Logf("  Workspace Files:   %d", len(student.Workspace))
		t.Logf("  Activity Logs:     %d entries", len(student.ActivityLog))
		t.Logf("================================================")

		fmt.Println("\n--- Full Activity Log ---")
		for _, entry := range student.ActivityLog {
			fmt.Printf("   %s\n", entry)
		}
	})
}

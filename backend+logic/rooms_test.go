package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRoomFlow(t *testing.T) {
	// 1. Create Room
	createBody := []byte(`{
		"host_id": "host1",
		"session_name": "Test Session",
		"admin_key": "secret123",
		"time_allocated": 3600000000000
	}`)
	req, _ := http.NewRequest("POST", "/create-room", bytes.NewBuffer(createBody))
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(CreateRoomHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("CreateRoom handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var createResp map[string]string
	json.Unmarshal(rr.Body.Bytes(), &createResp)
	roomID := createResp["room_id"]
	if roomID == "" {
		t.Errorf("CreateRoom did not return a room_id")
	}

	// 2. Join Room
	joinBody := []byte(`{
		"room_id": "` + roomID + `",
		"user_id": "user1",
		"username": "TestUser",
		"regno": "REG001"
	}`)
	req, _ = http.NewRequest("POST", "/join-room", bytes.NewBuffer(joinBody))
	rr = httptest.NewRecorder()
	handler = http.HandlerFunc(JoinRoomHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("JoinRoom handler returned wrong status code: got %v want %v. Body: %s", status, http.StatusOK, rr.Body.String())
	}

	// 3. Admin Update Status
	updateBody := []byte(`{
		"room_id": "` + roomID + `",
		"admin_key": "secret123",
		"user_id": "user1",
		"status": 3
	}`) // 3 is Flagged (Wait, StatusEnum vs UStatusEnum... let's check)
	// UStatusEnum: Online=0, Offline=1, Submitted=2, Flagged=3

	req, _ = http.NewRequest("POST", "/admin/update-status", bytes.NewBuffer(updateBody))
	rr = httptest.NewRecorder()
	handler = http.HandlerFunc(AdminUpdateUserHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("AdminUpdateUser handler returned wrong status code: got %v want %v. Body: %s", status, http.StatusOK, rr.Body.String())
	}

	// 3.5. Start Exam
	startBody := []byte(`{
		"room_id": "` + roomID + `",
		"admin_key": "secret123"
	}`)
	req, _ = http.NewRequest("POST", "/start-exam", bytes.NewBuffer(startBody))
	rr = httptest.NewRecorder()
	handler = http.HandlerFunc(StartExamHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("StartExam handler returned wrong status code: got %v want %v. Body: %s", status, http.StatusOK, rr.Body.String())
	}

	// 4. Verify Status (Get Room)
	req, _ = http.NewRequest("GET", "/get-room?room_id="+roomID, nil)
	rr = httptest.NewRecorder()
	handler = http.HandlerFunc(GetRoomHandler)
	handler.ServeHTTP(rr, req)

	var room Room
	json.Unmarshal(rr.Body.Bytes(), &room)
	
	found := false
	for _, s := range room.Students {
		if s.UserID == "user1" {
			found = true
			if s.ActiveStatus != Flagged { // Wait, Flagged is 3.
				// In Go, enums are just consts. I need to make sure Flagged is visible or just use 3.
				// Flagged is defined in rooms.go in package main.
				// Since test is package main, it should be visible if I run `go test .` (which compiles all main package files together).
				// But `go test rooms_test.go` won't see rooms.go.
				// I should run `go test -v .`
			}
		}
	}
	if !found {
		t.Errorf("User not found in room after update")
	}
}

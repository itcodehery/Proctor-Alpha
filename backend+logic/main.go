package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

type ScanResult struct {
	ForbiddenFound bool     `json:"forbidden_found"`
	Processes      []string `json:"processes"`
}

var forbiddenApps = []string{"firefox", "hotspotshield", "discord", "slack", "zen"}
var wsHub *Hub

func serveWsHandler(w http.ResponseWriter, r *http.Request) {
	serveWs(wsHub, w, r)
}

func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

func enableCors(w *http.ResponseWriter) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT")
	(*w).Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func checkProcessesHandler(w http.ResponseWriter, r *http.Request) {
	enableCors(&w)
	if r.Method == "OPTIONS" {
		return
	}
	roomID := r.URL.Query().Get("room_id")
	targetApps := forbiddenApps
	if roomID != "" {
		mu.RLock()
		room, exists := rooms[roomID]
		if exists && len(room.ForbiddenApps) > 0 {
			targetApps = room.ForbiddenApps
		}
		mu.RUnlock()
	}
	cmd := exec.Command("ps", "-e")
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("Error running ps:", err)
		http.Error(w, "Failed to scan processes", http.StatusInternalServerError)
		return
	}
	outStr := strings.ToLower(string(output))
	found := []string{}
	for _, app := range targetApps {
		if strings.Contains(outStr, app) {
			found = append(found, app)
		}
	}
	result := ScanResult{
		ForbiddenFound: len(found) > 0,
		Processes:      found,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func main() {
	ip := GetLocalIP()
	fmt.Printf("Starting Proctor Backend on :8081...\n")
	if ip != "" {
		fmt.Printf("Admin: Share this IP with students: %s\n", ip)
	}

	// Initialize WebSocket Hub
	wsHub = newHub()
	go wsHub.run()

	// Start offline detection: sweep every 30s, mark offline after 45s of no ping
	startOfflineDetector(30*time.Second, 45*time.Second)

	// WebSocket
	http.HandleFunc("/ws", serveWsHandler)

	// Utility
	http.HandleFunc("/scan", checkProcessesHandler)

	// Room management
	http.HandleFunc("/create-room", CreateRoomHandler)
	http.HandleFunc("/join-room", JoinRoomHandler)
	http.HandleFunc("/start-exam", StartExamHandler)
	http.HandleFunc("/get-room", GetRoomHandler)
	http.HandleFunc("/get-all-rooms", GetAllRoomsHandler)
	http.HandleFunc("/update-room", UpdateRoomHandler)
	http.HandleFunc("/timer-info", TimerInfoHandler)

	// Student actions
	http.HandleFunc("/student/ping", PingHandler)
	http.HandleFunc("/student/snapshot", SnapshotHandler)
	http.HandleFunc("/sync-code", SyncCodeHandler)
	http.HandleFunc("/submit", SubmitHandler)
	http.HandleFunc("/log-activity", LogActivityHandler)

	// Admin actions
	http.HandleFunc("/admin/update-status", AdminUpdateUserHandler)
	http.HandleFunc("/admin/kick-student", KickStudentHandler)

	// Discovery endpoint — students ping this to find a proctor on the LAN
	http.HandleFunc("/discover", func(w http.ResponseWriter, r *http.Request) {
		enableCors(&w)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"service": "proctor-alpha",
			"ip":      ip,
			"status":  "online",
		})
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		enableCors(&w)
		fmt.Fprintf(w, "Proctor Backend Active. Use /scan to check processes.")
	})

	err := http.ListenAndServe(":8081", nil)
	if err != nil {
		fmt.Println("Error starting server:", err)
	}
}

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
)

type ScanResult struct {
	ForbiddenFound bool     `json:"forbidden_found"`
	Processes      []string `json:"processes"`
}

var forbiddenApps = []string{"firefox", "hotspotshield", "discord", "slack", "spotify", "zen"}

func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
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

	// Run ps command to list all processes
	// Using "-e" for standard syntax to select all processes
	cmd := exec.Command("ps", "-e")
	output, err := cmd.Output()
	if err != nil {
		// Fallback or error handling
		fmt.Println("Error running ps:", err)
		http.Error(w, "Failed to scan processes", http.StatusInternalServerError)
		return
	}

	outStr := strings.ToLower(string(output))
	found := []string{}

	for _, app := range forbiddenApps {
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
	fmt.Printf("Starting Proctor Process Shield on :8080...\n")
	if ip != "" {
		fmt.Printf("Admin: Share this IP with students: %s\n", ip)
	}

	http.HandleFunc("/scan", checkProcessesHandler)
	http.HandleFunc("/create-room", CreateRoomHandler)
	http.HandleFunc("/join-room", JoinRoomHandler)
	http.HandleFunc("/start-exam", StartExamHandler)
	http.HandleFunc("/admin/update-status", AdminUpdateUserHandler)
	http.HandleFunc("/get-room", GetRoomHandler)
	http.HandleFunc("/get-all-rooms", GetAllRoomsHandler)
	http.HandleFunc("/update-room", UpdateRoomHandler)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		enableCors(&w)
		fmt.Fprintf(w, "Proctor Backend Active. Use /scan to check processes.")
	})

	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		fmt.Println("Error starting server:", err)
	}
}

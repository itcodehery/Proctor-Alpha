package main

import (
	"fmt"
	"net/http"
)

// Logic function to reverse a string
func Reverse(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

func main() {
	fmt.Println("Starting backend server on :8080...")

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		message := "Hello from Go Backend!"
		reversed := Reverse(message)
		fmt.Fprintf(w, "Original: %s\nReversed: %s\n", message, reversed)
	})

	// Sample of some backend logic being executed
	testStr := "Go is awesome"
	fmt.Printf("Sample Logic: Reversed '%s' is '%s'\n", testStr, Reverse(testStr))

	// In a real scenario, we'd start the server. 
	// For this sample, we'll just print that it's ready.
	// err := http.ListenAndServe(":8080", nil)
	// if err != nil {
	// 	fmt.Println("Error starting server:", err)
	// }
}

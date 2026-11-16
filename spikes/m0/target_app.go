package main

import (
	"fmt"
	"net/http"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, "candidate-healthy")
	})
	fmt.Println("Target app listening on :8080")
	http.ListenAndServe(":8080", nil)
}

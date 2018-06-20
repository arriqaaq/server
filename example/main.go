package main

import (
	"fmt"
	lucio "github.com/arriqaaq/server"
	"log"
	"net/http"
	"os"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(w, "Hello from %v!\n", os.Getpid())
	})
	server := lucio.NewServer(mux, "0.0.0.0", 8080)
	err := server.Serve()
	log.Println("terminated", os.Getpid(), err)
}

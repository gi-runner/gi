package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gi-runner/gi/internal/demoapi"
)

func main() {
	addr := flag.String("addr", ":18080", "listen address")
	flag.Parse()
	fmt.Printf("users api listening on %s\n", *addr)
	srv := &http.Server{
		Addr:              *addr,
		Handler:           demoapi.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}

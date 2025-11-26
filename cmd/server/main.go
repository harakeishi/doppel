package main

import (
	"database/sql"
	"flag"
	"log"
	"net/http"

	"github.com/example/doppel/internal/server"
	_ "modernc.org/sqlite"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dsn := flag.String("db", "file:shadow.db?_pragma=busy_timeout=5000", "SQLite DSN")
	flag.Parse()

	db, err := sql.Open("sqlite", *dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	handler, err := server.NewServer(db)
	if err != nil {
		log.Fatalf("init server: %v", err)
	}

	log.Printf("starting shadow server on %s", *addr)
	if err := http.ListenAndServe(*addr, handler); err != nil {
		log.Fatal(err)
	}
}

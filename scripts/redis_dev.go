// Package main provides a standalone in-memory Redis service for local development.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/alicebob/miniredis/v2"
)

func main() {
	s := miniredis.NewMiniRedis()
	if err := s.StartAddr("127.0.0.1:6379"); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start miniredis: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()
	fmt.Printf("Embedded Redis server running on %s\n", s.Addr())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	fmt.Println("Shutting down Redis server...")
}

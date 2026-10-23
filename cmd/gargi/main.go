// Package main provides gargi, the developer CLI that validates simulation
// models and runs simulation experiments in-process.
package main

import "os"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

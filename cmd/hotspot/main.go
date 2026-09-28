package main

import (
	"os"

	"github.com/luismasuarez/hotspot/internal/hotspot"
)

func main() {
	os.Exit(hotspot.Main(os.Args[1:], os.Stdout, os.Stderr))
}

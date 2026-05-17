package main

import (
	"context"
	"os"

	"github.com/Dave-lab12/ark/internal"
)

func main() {
	os.Exit(internal.Main(context.Background(), os.Args[1:]))
}

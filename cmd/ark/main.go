package main

import (
	"context"
	"os"

	"ark/internal"
)

func main() {
	os.Exit(internal.Main(context.Background(), os.Args[1:]))
}

package main

import (
"embed"
"log"

"github.com/MardukPrime/Fast-V/internal/bootstrap"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
if err := bootstrap.Run(assets); err != nil {
log.Fatal(err)
}
}

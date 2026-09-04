package main

import (
	"log"

	"github.com/septrum101/zteOnu/cmd"
)

func main() {
	// Double-clicked in Explorer (no CLI args): open the native GUI directly.
	if cmd.AutoLaunchGUI() {
		return
	}
	if err := cmd.Execute(); err != nil {
		log.Panicln(err)
	}
}

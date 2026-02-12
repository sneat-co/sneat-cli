package main

import (
	"fmt"
	"github.com/sneat-co/sneat-tui/sneatui"
	"os"
)

// test hooks to allow overriding in tests
var (
	getApplication = newApplication
	exit           = os.Exit
)

func main() {
	app := getApplication()
	if err := app.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		exit(1)
	}
}

type application interface {
	Run() error
}

func newApplication() application {
	return sneatui.NewApp()
}

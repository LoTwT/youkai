// Command youkai is the entry point of the Youkai application. It only
// assembles the app and opens the main window; feature packages are wired
// in here as later milestones add them.
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "Youkai",
			Width:   480,
			Height:  320,
			Content: ui.View(view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// view is the placeholder content of the main window until the real
// interface is built.
func view(c *ui.Context) {
	ui.Column(c).Fill().Center().Children(func() {
		ui.Text(c, "Youkai")
	})
}

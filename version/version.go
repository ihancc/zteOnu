package version

import (
	"fmt"
)

var (
	version = "dev"
	appName = "ZteONU"
	date    = "unknown"
	intro   = "https://github.com/septrum101/zteOnu"
)

func Show() {
	fmt.Printf("%s %s, built at %s\nsource: %s\n", appName, version, date, intro)
}

// Line returns the one-line version banner without the source URL, for UIs that
// show it in a compact space.
func Line() string {
	return fmt.Sprintf("%s %s, built at %s", appName, version, date)
}

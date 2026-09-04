//go:build tools

// This file exists only to keep golang.org/x/mobile in go.mod so that
// `gomobile bind` can build the Android binding. The "tools" build tag means it
// is never compiled into the app or the desktop build.
package zteonu

import _ "golang.org/x/mobile/bind"

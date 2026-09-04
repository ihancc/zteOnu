//go:build windows

package cmd

import (
	"os"
	"syscall"
	"unsafe"
)

// AutoLaunchGUI opens the GUI and returns true when the program was started by
// double-clicking it (no CLI arguments and not attached to an existing shell's
// console). Started from a terminal, or with any argument, it returns false so
// the normal command line runs.
func AutoLaunchGUI() bool {
	if len(os.Args) != 1 {
		return false
	}
	// More than one process on the console means a shell launched us, so keep
	// the command-line behavior. Zero (GUI subsystem) or one (Explorer spawned
	// the console just for us) means a double-click: open the GUI.
	if consoleProcessCount() > 1 {
		return false
	}
	runGUI()
	return true
}

// consoleProcessCount reports how many processes share this process's console.
func consoleProcessCount() int {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleProcessList")
	var pids [8]uint32
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	return int(n)
}

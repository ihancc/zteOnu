//go:build !windows

package cmd

// AutoLaunchGUI is a no-op on non-Windows platforms; the CLI always runs.
func AutoLaunchGUI() bool { return false }

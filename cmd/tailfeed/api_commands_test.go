package main

import "testing"

func TestRootDoesNotExposeRemovedAPICommands(t *testing.T) {
	for _, command := range rootCmd().Commands() {
		if command.Name() == "register" || command.Name() == "usage" {
			t.Errorf("removed API command %q is still registered", command.Name())
		}
	}
}

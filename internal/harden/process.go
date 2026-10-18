package harden

import "runtime/debug"

// Process stops core dumps and debugger attachment and silences panic
// tracebacks, which could print secrets. It is best effort: an attacker
// running as the same user can attach before it runs, which is what
// Contained mode is for.
func Process() error {
	debug.SetTraceback("none")
	return lockProcess()
}

//go:build !unix

package main

// Non-Unix platforms do not need a SIGPIPE disposition for output errors.
func configureOutputSignals() {}

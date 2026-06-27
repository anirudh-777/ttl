package update

import "runtime"

// runtimeGOOS and runtimeGOARCH are tiny indirection so the test
// imports runtime in exactly one file. They must match runtime.GOOS
// and runtime.GOARCH respectively.
func runtimeGOOS() string   { return runtime.GOOS }
func runtimeGOARCH() string { return runtime.GOARCH }

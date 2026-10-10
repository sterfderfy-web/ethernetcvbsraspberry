package main

import "fmt"

// Linux service uses the verified stop-file protocol instead of a Windows handle.
func watchPatcherParent(pid int, stop string) (func(), error) {
    return nil, fmt.Errorf("parent-pid is Windows-only; use stop-file on Linux")
}

//go:build !darwin && !linux

package agent

import (
	"errors"
	"os"
)

func openNoFollow(root *os.Root, name string) (*os.File, error) {
	return nil, errors.New("alpha Agent supports macOS and Linux only")
}
func acquireLock(path string) (func(), error) {
	return nil, errors.New("alpha Agent supports macOS and Linux only")
}

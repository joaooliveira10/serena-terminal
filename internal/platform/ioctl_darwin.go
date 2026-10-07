//go:build darwin

package platform

import "syscall"

// ioctlGetTermios is the request that reads terminal attributes.
const ioctlGetTermios = syscall.TIOCGETA

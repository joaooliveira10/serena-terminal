//go:build linux

package platform

import "syscall"

// ioctlGetTermios is the request that reads terminal attributes.
const ioctlGetTermios = syscall.TCGETS

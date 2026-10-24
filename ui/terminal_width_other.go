//go:build !darwin && !linux && !freebsd

package ui

import "io"

func terminalColumns(io.Writer) int { return 0 }

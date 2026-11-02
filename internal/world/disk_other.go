//go:build !unix && !windows

package world

func freeBytes(string) (uint64, bool) { return 0, false }

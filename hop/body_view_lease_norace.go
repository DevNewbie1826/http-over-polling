//go:build !race

package hop

func bodyViewUsesReadLease() bool { return true }

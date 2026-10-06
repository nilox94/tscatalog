//go:build !unix

package lifecycle

func detachedAlive(name string, index int) bool {
	return false
}

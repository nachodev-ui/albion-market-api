//go:build !windows

package observability

import "os"

func prepareColorOutput(_ *os.File) bool {
	return true
}

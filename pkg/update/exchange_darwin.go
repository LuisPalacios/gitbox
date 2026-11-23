//go:build darwin

package update

import "golang.org/x/sys/unix"

func bundleExchange() func(string, string) error {
	return func(a, b string) error { return unix.RenamexNp(a, b, unix.RENAME_SWAP) }
}

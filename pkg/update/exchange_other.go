//go:build !darwin

package update

func bundleExchange() func(string, string) error { return nil }

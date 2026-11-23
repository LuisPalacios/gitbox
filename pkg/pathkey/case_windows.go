package pathkey

import "strings"

func comparison(abs, _ string, _ bool) string { return strings.ToLower(abs) }

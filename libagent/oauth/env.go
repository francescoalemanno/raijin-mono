package oauth

import "os"

// osGetenv is a thin wrapper around os.Getenv kept in its own file so it is
// easy to swap in tests without importing os everywhere.
func osGetenv(key string) string {
	return os.Getenv(key)
}

//go:build !linux

package disk

// Usage is a no-op fallback so the project still builds on developer
// machines (Windows/macOS); the deployed container reports real usage.
func Usage(path string) (total, free, avail uint64, err error) {
	return 0, 0, 0, nil
}

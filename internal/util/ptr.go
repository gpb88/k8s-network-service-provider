// Package util provides general-purpose helper functions.
package util

// Ptr returns a pointer to the given value.
func Ptr[T any](v T) *T { return &v }

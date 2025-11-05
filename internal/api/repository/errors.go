package repository

import "errors"

// Sentinel errors to allow upper layers to perform reliable comparisons.
var (
	ErrWordNotFound    = errors.New("word not found")
	ErrVariantNotFound = errors.New("variant not found")
)

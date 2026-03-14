package contracts

import "errors"

var (
	ErrWordNotFound    = errors.New("word not found")
	ErrVariantNotFound = errors.New("variant not found")
)

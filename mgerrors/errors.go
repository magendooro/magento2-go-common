// Package mgerrors provides Magento-compatible error messages shared across services.
// All user-facing error strings match Magento PHP exactly.
package mgerrors

import "fmt"

// ErrUnauthorized is returned when an operation requires authentication
// and no valid customer session is present.
var ErrUnauthorized = fmt.Errorf("The current customer isn't authorized.")

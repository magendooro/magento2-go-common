package mgerrors

import "testing"

func TestErrUnauthorized_Message(t *testing.T) {
	want := "The current customer isn't authorized."
	if ErrUnauthorized.Error() != want {
		t.Errorf("ErrUnauthorized = %q, want %q", ErrUnauthorized.Error(), want)
	}
}

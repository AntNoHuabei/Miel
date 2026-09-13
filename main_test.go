package main

import (
	"errors"
	"testing"
)

func TestIsDialogCancelledError(t *testing.T) {
	if !isDialogCancelledError(errors.New("cancelled by user")) {
		t.Fatal("cancelled dialog error was not recognized")
	}
	if !isDialogCancelledError(errors.New("canceled by user")) {
		t.Fatal("canceled dialog error was not recognized")
	}
	if isDialogCancelledError(errors.New("permission denied")) {
		t.Fatal("non-cancellation error was swallowed")
	}
}

package updater

import (
	"errors"
	"strings"
	"testing"
)

func TestGetChangesWrapsError(t *testing.T) {
	// Use a tag containing a control character to trigger an http.Get URL parse error.
	// This reliably fails at the network/URL layer without needing external connectivity.
	_, err := getChanges("__nonexistent__\n")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify the error wraps the original HTTP/URL error.
	// Before the fix, errors.New("check your internet connection") does NOT wrap.
	// After the fix, fmt.Errorf with %w should preserve the original error in the chain.
	if unwrapped := errors.Unwrap(err); unwrapped == nil {
		t.Error("expected wrapped error (errors.Unwrap should return the original error)")
	}

	// Verify the error message contains the original error detail, not just the static message.
	if !strings.Contains(err.Error(), "invalid control character") {
		t.Errorf("expected error to contain the original URL error, got: %v", err)
	}
}

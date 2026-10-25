package cli

import (
	"os"
	"shepherd/internal/testenv"
	"testing"
)

// TestMain sandboxes the environment so no test reaches the real boards or config.
func TestMain(m *testing.M) {
	os.Exit(testenv.Run(m.Run))
}

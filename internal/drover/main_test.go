package drover

import (
	"os"
	"shepherd/internal/testenv"
	"testing"
)

// TestMain sandboxes the environment so no test reaches the real boards, config or drover.
func TestMain(m *testing.M) {
	os.Exit(testenv.Run(m.Run))
}

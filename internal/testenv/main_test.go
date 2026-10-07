package testenv

import "testing"

// TestMain gives every test of this package the hermetic home of rule T1.
func TestMain(m *testing.M) { Main(m) }

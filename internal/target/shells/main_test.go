package shells

import (
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// standInEnv makes this test program play the part of a PowerShell when the
// probe tests start it as a child: with the value "hang" it never answers,
// with any other value it prints that value and ends. No real PowerShell is
// started by any test, and the child touches no file. The variable counts
// only together with the arguments of the probe, so that it cannot switch the
// tests off when it is left in somebody's environment.
const standInEnv = "SERENO_TEST_STAND_IN"

// TestMain gives every test of this package the hermetic home of rule T1.
func TestMain(m *testing.M) {
	answer := os.Getenv(standInEnv)
	if answer == "" || !reflect.DeepEqual(os.Args[1:], ProbeArgs()) {
		testenv.Main(m)
		return
	}
	if answer == "hang" {
		time.Sleep(time.Minute)
	} else {
		os.Stdout.WriteString(answer)
	}
	os.Exit(0)
}

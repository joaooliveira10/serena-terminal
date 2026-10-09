package shells

import (
	"os"
	"reflect"
	"strings"
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

// standInPolicy in front of a profile path makes the stand-in answer with
// that path and with the execution policy it finds in its own environment.
const standInPolicy = "policy of the environment:"

// TestMain gives every test of this package the hermetic home of rule T1.
func TestMain(m *testing.M) {
	answer := os.Getenv(standInEnv)
	if answer == "" || !reflect.DeepEqual(os.Args[1:], ProbeArgs()) {
		testenv.Main(m)
		return
	}
	if answer == "hang" {
		time.Sleep(time.Minute)
	} else if profile, asked := strings.CutPrefix(answer, standInPolicy); asked {
		// As a PowerShell answers: the profile path, then the policy that
		// was handed down to it, or none.
		policy := os.Getenv(policyPreferenceEnv)
		if policy == "" {
			policy = "Undefined"
		}
		os.Stdout.WriteString(profile + "\r\n" + policy + "\r\n")
	} else {
		os.Stdout.WriteString(answer)
	}
	os.Exit(0)
}

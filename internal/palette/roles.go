package palette

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

//go:embed roles.json
var rolesJSON []byte

// Role names one use of colour above the emulator. Roles map to SGR
// parameters, never to hex values ("slots, not hex", R8).
type Role string

// The roles of R8.
const (
	RoleFunction     Role = "function" // function and command names
	RoleKeyword      Role = "keyword"
	RoleString       Role = "string"
	RoleType         Role = "type"
	RoleProperty     Role = "property" // properties and members
	RoleConstant     Role = "constant" // constants and numbers
	RoleParameter    Role = "parameter"
	RoleOperator     Role = "operator"
	RoleVariable     Role = "variable"
	RoleDefault      Role = "default" // text with no other role
	RoleComment      Role = "comment"
	RoleError        Role = "error"
	RoleEmphasis     Role = "emphasis"
	RoleSelection    Role = "selection" // reverse video, never a painted band
	RolePromptPath   Role = "prompt.path"
	RolePromptBranch Role = "prompt.branch"
	RolePromptOK     Role = "prompt.ok"     // prompt character after a successful command
	RolePromptFailed Role = "prompt.failed" // prompt character after a failed command
	RolePromptMeta   Role = "prompt.meta"   // duration and other secondary prompt text
)

// roleOrder lists the roles in the order of R8.
var roleOrder = []Role{
	RoleFunction, RoleKeyword, RoleString, RoleType, RoleProperty, RoleConstant,
	RoleParameter, RoleOperator, RoleVariable, RoleDefault, RoleComment, RoleError,
	RoleEmphasis, RoleSelection, RolePromptPath, RolePromptBranch, RolePromptOK,
	RolePromptFailed, RolePromptMeta,
}

// RoleList returns every role in the order of R8.
func RoleList() []Role { return append([]Role(nil), roleOrder...) }

// Roles is the content of roles.json: the SGR parameters of every role.
type Roles struct {
	sgr map[Role]string
}

// LoadRoles returns the embedded role table.
func LoadRoles() (*Roles, error) {
	r, err := ParseRoles(rolesJSON)
	if err != nil {
		return nil, fmt.Errorf("embedded roles.json: %w", err)
	}
	return r, nil
}

// ParseRoles decodes a roles.json file. It requires exactly the roles of R8
// and well-formed SGR parameter lists; whether each parameter is allowed is
// the business of the contrast gate (Check), which reports instead of failing.
func ParseRoles(data []byte) (*Roles, error) {
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	roles := &Roles{sgr: make(map[Role]string, len(roleOrder))}
	for _, role := range roleOrder {
		params, ok := raw[string(role)]
		if !ok {
			return nil, fmt.Errorf("role %q is missing", role)
		}
		if _, err := ParseSGR(params); err != nil {
			return nil, fmt.Errorf("role %q: %w", role, err)
		}
		roles.sgr[role] = params
		delete(raw, string(role))
	}
	for name := range raw {
		return nil, fmt.Errorf("unknown role %q", name)
	}
	return roles, nil
}

// SGR returns the parameters of a role, for example "3;37".
func (r *Roles) SGR(role Role) string { return r.sgr[role] }

// Sequence returns the complete escape sequence that switches a role on:
// ESC [ parameters m.
func (r *Roles) Sequence(role Role) string { return esc + "[" + r.sgr[role] + "m" }

// ParseSGR splits a parameter list such as "3;37" into its numbers.
func ParseSGR(params string) ([]int, error) {
	if params == "" {
		return nil, fmt.Errorf("empty SGR parameter list")
	}
	var numbers []int
	for _, field := range strings.Split(params, ";") {
		n, err := strconv.Atoi(field)
		if err != nil || n < 0 || field != strconv.Itoa(n) {
			return nil, fmt.Errorf("%q is not a list of SGR parameters", params)
		}
		numbers = append(numbers, n)
	}
	return numbers, nil
}

// AllowedSGR reports whether a parameter may appear in anything sereno
// generates above the emulator (R8): 0, 1, 3, 4, 7, 22, 23, 24, 27, 30-37, 39
// and 90-97.
func AllowedSGR(n int) bool {
	switch n {
	case 0, 1, 3, 4, 7, 22, 23, 24, 27, 39:
		return true
	}
	return (n >= 30 && n <= 37) || (n >= 90 && n <= 97)
}

// ForbiddenSGR reports whether a parameter paints a background or leaves the
// sixteen slots: 38, 48, 40-47, 49 and 100-107. Generated files are scanned
// for these (R8); they are the reason "reverse, do not paint" exists.
func ForbiddenSGR(n int) bool {
	return n == 38 || n == 48 || (n >= 40 && n <= 47) || n == 49 || (n >= 100 && n <= 107)
}

// CheckSGR returns an error naming the first parameter of the list that is
// not allowed by R8.
func CheckSGR(params string) error {
	numbers, err := ParseSGR(params)
	if err != nil {
		return err
	}
	for _, n := range numbers {
		if !AllowedSGR(n) {
			return fmt.Errorf("SGR parameter %d is not allowed", n)
		}
	}
	return nil
}

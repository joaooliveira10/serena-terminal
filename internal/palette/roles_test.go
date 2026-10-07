package palette

import (
	"strings"
	"testing"
)

// TestRolesMatchTheSpecification spells out the table of R8.
func TestRolesMatchTheSpecification(t *testing.T) {
	want := []struct {
		role Role
		sgr  string
	}{
		{RoleFunction, "34"}, {RoleKeyword, "35"}, {RoleString, "32"}, {RoleType, "33"},
		{RoleProperty, "36"}, {RoleConstant, "93"}, {RoleParameter, "3;37"}, {RoleOperator, "37"},
		{RoleVariable, "39"}, {RoleDefault, "39"}, {RoleComment, "90"}, {RoleError, "91"},
		{RoleEmphasis, "96"}, {RoleSelection, "7"}, {RolePromptPath, "34"}, {RolePromptBranch, "35"},
		{RolePromptOK, "32"}, {RolePromptFailed, "31"}, {RolePromptMeta, "90"},
	}
	roles := loadRoles(t)
	list := RoleList()
	if len(list) != len(want) {
		t.Fatalf("%d roles, want %d", len(list), len(want))
	}
	for i, w := range want {
		if list[i] != w.role {
			t.Errorf("role %d is %q, want %q", i, list[i], w.role)
		}
		if got := roles.SGR(w.role); got != w.sgr {
			t.Errorf("%s = %q, want %q", w.role, got, w.sgr)
		}
		if err := CheckSGR(roles.SGR(w.role)); err != nil {
			t.Errorf("%s: %v", w.role, err)
		}
	}
	if got := roles.Sequence(RoleParameter); got != "\x1b[3;37m" {
		t.Errorf("Sequence(parameter) = %q", got)
	}
}

func TestParseRolesRejects(t *testing.T) {
	valid := string(rolesJSON)
	tests := map[string]string{
		"missing role":   strings.Replace(valid, `  "keyword": "35",`+"\n", "", 1),
		"unknown role":   strings.Replace(valid, `"keyword": "35",`, `"keyword": "35", "banner": "1",`, 1),
		"empty value":    strings.Replace(valid, `"keyword": "35"`, `"keyword": ""`, 1),
		"not a number":   strings.Replace(valid, `"keyword": "35"`, `"keyword": "bold"`, 1),
		"trailing colon": strings.Replace(valid, `"keyword": "35"`, `"keyword": "35;"`, 1),
		"leading zero":   strings.Replace(valid, `"keyword": "35"`, `"keyword": "035"`, 1),
		"not json":       "roles",
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if input == valid {
				t.Fatal("the edit did not change the input")
			}
			if _, err := ParseRoles([]byte(input)); err == nil {
				t.Error("ParseRoles accepted the file")
			}
		})
	}
}

// TestSGRSets checks the allowed and the forbidden parameters of R8 number by
// number.
func TestSGRSets(t *testing.T) {
	allowed := map[int]bool{0: true, 1: true, 3: true, 4: true, 7: true, 22: true, 23: true, 24: true, 27: true, 39: true}
	for n := 30; n <= 37; n++ {
		allowed[n] = true
	}
	for n := 90; n <= 97; n++ {
		allowed[n] = true
	}
	forbidden := map[int]bool{38: true, 48: true, 49: true}
	for n := 40; n <= 47; n++ {
		forbidden[n] = true
	}
	for n := 100; n <= 107; n++ {
		forbidden[n] = true
	}
	for n := 0; n <= 120; n++ {
		if AllowedSGR(n) != allowed[n] {
			t.Errorf("AllowedSGR(%d) = %v", n, AllowedSGR(n))
		}
		if ForbiddenSGR(n) != forbidden[n] {
			t.Errorf("ForbiddenSGR(%d) = %v", n, ForbiddenSGR(n))
		}
		if AllowedSGR(n) && ForbiddenSGR(n) {
			t.Errorf("%d is both allowed and forbidden", n)
		}
	}
}

func TestCheckSGR(t *testing.T) {
	for _, ok := range []string{"0", "34", "3;37", "1;34", "4;34", "7", "93"} {
		if err := CheckSGR(ok); err != nil {
			t.Errorf("CheckSGR(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"44;1", "38;5;12", "48;2;0;0;0", "34;42", "107", "49", "2", "", "x"} {
		if err := CheckSGR(bad); err == nil {
			t.Errorf("CheckSGR(%q) succeeded", bad)
		}
	}
}

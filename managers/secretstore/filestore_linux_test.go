package secretstore

import "testing"

func TestValidateUID(t *testing.T) {
	for _, ok := range []string{"0", "1000", "4294967295"} {
		if err := ValidateUID(ok); err != nil {
			t.Errorf("ValidateUID(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-1", "..", "1000/..", "S-1-5-18", "4294967296"} {
		if ValidateUID(bad) == nil {
			t.Errorf("ValidateUID(%q) accepted", bad)
		}
	}
}

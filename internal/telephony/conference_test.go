package telephony

import "testing"

func TestConferenceIdentityIsPartyScopedAndRoundTrips(t *testing.T) {
	name, err := ConferenceName("pty_family-a", "102")
	if err != nil || name != "rrc-pty_family-a-102" {
		t.Fatalf("ConferenceName = %q, %v", name, err)
	}
	partyID, extension, ok := ParseConferenceName(name)
	if !ok || partyID != "pty_family-a" || extension != "102" || JoinNumber(extension) != "*16102" {
		t.Fatalf("ParseConferenceName = %q, %q, %t", partyID, extension, ok)
	}
	for _, unsafe := range []struct{ partyID, extension string }{
		{"pty_other\n[evil]", "102"}, {"pty_safe", "911"}, {"pty_safe", "10a"},
	} {
		if _, err := ConferenceName(unsafe.partyID, unsafe.extension); err == nil {
			t.Fatalf("unsafe conference identity was accepted: %#v", unsafe)
		}
	}
	for _, invalid := range []string{"rrc-pty_safe-911", "rrc-pty_safe-10a", "other-pty_safe-102", "rrc--102"} {
		if _, _, ok := ParseConferenceName(invalid); ok {
			t.Fatalf("invalid conference name was accepted: %q", invalid)
		}
	}
}

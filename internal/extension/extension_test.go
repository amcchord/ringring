package extension

import (
	"strconv"
	"testing"
)

func TestValidRejectsPublicSafetyAndMalformedNumbers(t *testing.T) {
	for _, value := range []string{"10", "101", "110", "120", "1000", "99999"} {
		if !Valid(value) {
			t.Errorf("ordinary party extension %q was rejected", value)
		}
	}
	for _, value := range []string{"", "1", "123456", "10*", "１２３", "000", "111", "112", "911", "988", "999"} {
		if Valid(value) {
			t.Errorf("unsafe extension %q was accepted", value)
		}
	}
	if Reserved("101") || !Reserved("911") || !Reserved("988") {
		t.Fatal("reserved-number classification did not match validation")
	}
}

func TestSuggestSkipsUsedAndReservedNumbers(t *testing.T) {
	if got := Suggest(nil); got != "101" {
		t.Fatalf("empty-party suggestion = %q", got)
	}
	usedBeforeReserved := make([]string, 0, 10)
	for number := 101; number <= 110; number++ {
		usedBeforeReserved = append(usedBeforeReserved, strconv.Itoa(number))
	}
	if got := Suggest(usedBeforeReserved); got != "113" {
		t.Fatalf("suggestion did not skip reserved 111 and 112: %q", got)
	}
	used := make([]string, 0, 13)
	for number := 101; number <= 113; number++ {
		used = append(used, strconv.Itoa(number))
	}
	if got := Suggest(used); got != "114" {
		t.Fatalf("suggestion with occupied/reserved range = %q", got)
	}
}

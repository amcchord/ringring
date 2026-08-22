// Package extension owns the safety and formatting rules for party numbers.
package extension

import "strconv"

// reservedPublicSafetyNumbers are deliberately unavailable inside a party so
// a family member can never appear to answer a familiar emergency or crisis
// number. RingRing still cannot route any number to a public telephone network.
var reservedPublicSafetyNumbers = map[string]struct{}{
	"000": {}, // Australia emergency
	"111": {}, // New Zealand emergency
	"112": {}, // European emergency
	"911": {}, // United States/Canada emergency
	"988": {}, // United States crisis lifeline
	"999": {}, // United Kingdom emergency
}

// Valid reports whether value can be assigned to a RingRing party member.
func Valid(value string) bool {
	if len(value) < 2 || len(value) > 5 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return !Reserved(value)
}

// Reserved reports whether value is a familiar public emergency or crisis
// number that must never identify a family member inside RingRing.
func Reserved(value string) bool {
	_, reserved := reservedPublicSafetyNumbers[value]
	return reserved
}

// Suggest returns the first familiar three-or-more-digit extension not already
// used by a party. The database uniqueness constraint remains authoritative if
// two invitees submit the same suggestion concurrently.
func Suggest(used []string) string {
	taken := make(map[string]struct{}, len(used))
	for _, value := range used {
		taken[value] = struct{}{}
	}
	for number := 101; number <= 99999; number++ {
		candidate := strconv.Itoa(number)
		if !Valid(candidate) {
			continue
		}
		if _, exists := taken[candidate]; !exists {
			return candidate
		}
	}
	return ""
}

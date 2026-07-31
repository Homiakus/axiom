package runtime

import "testing"

func TestCloneAnyCopiesTypedCollections(t *testing.T) {
	type values []int
	type groups map[string][]int

	originalValues := values{1, 2, 3}
	clonedValues := CloneAny(originalValues).(values)
	clonedValues[0] = 99
	if originalValues[0] != 1 {
		t.Fatalf("typed slice was aliased: %#v", originalValues)
	}

	originalGroups := groups{"one": {1, 2, 3}}
	clonedGroups := CloneAny(originalGroups).(groups)
	clonedGroups["one"][0] = 99
	if originalGroups["one"][0] != 1 {
		t.Fatalf("typed map value was aliased: %#v", originalGroups)
	}
}

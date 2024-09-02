package dns

import (
	"testing"
)

func Test_ResourceGroupTags(t *testing.T) {
	testValue := "test-value"
	testValue2 := "test-value2"

	testCases := []struct {
		name    string
		oldTags map[string]*string
		newTags map[string]*string
		equal   bool
		merged  map[string]*string
	}{
		{
			name: "case0: Equal tags",
			oldTags: map[string]*string{
				"test-key": &testValue,
			},
			newTags: map[string]*string{
				"test-key": &testValue,
			},
			equal: true,
			merged: map[string]*string{
				"test-key": &testValue,
			},
		},
		{
			name:    "case1: Both are nil",
			oldTags: nil,
			newTags: nil,
			equal:   true,
			merged:  nil,
		},
		{
			name: "case2: Different tags",
			oldTags: map[string]*string{
				"test-key": &testValue,
			},
			newTags: map[string]*string{
				"test-key1": &testValue,
			},
			equal: false,
			merged: map[string]*string{
				"test-key":  &testValue,
				"test-key1": &testValue,
			},
		},
		{
			name: "case3: Different values",
			oldTags: map[string]*string{
				"test-key": &testValue,
			},
			newTags: map[string]*string{
				"test-key": &testValue2,
			},
			equal: false,
			merged: map[string]*string{
				"test-key": &testValue2,
			},
		},
		{
			name:    "case4: Old tags are nil",
			oldTags: nil,
			newTags: map[string]*string{
				"test-key": &testValue,
			},
			equal: false,
			merged: map[string]*string{
				"test-key": &testValue,
			},
		},
		{
			name: "case5: New tags are nil",
			oldTags: map[string]*string{
				"test-key": &testValue,
			},
			newTags: nil,
			equal:   false,
			merged: map[string]*string{
				"test-key": &testValue,
			},
		},
		{
			name:    "case6: Old tags are empty",
			oldTags: map[string]*string{},
			newTags: map[string]*string{
				"test-key": &testValue,
			},
			equal: false,
			merged: map[string]*string{
				"test-key": &testValue,
			},
		},
		{
			name: "case7: New tags are empty",
			oldTags: map[string]*string{
				"test-key": &testValue,
			},
			newTags: map[string]*string{},
			equal:   false,
			merged: map[string]*string{
				"test-key": &testValue,
			},
		},
		{
			name: "case8: Old tags contains various tags",
			oldTags: map[string]*string{
				"test-key":  &testValue,
				"test-key1": &testValue,
			},
			newTags: map[string]*string{
				"test-key": &testValue2,
			},
			equal: false,
			merged: map[string]*string{
				"test-key":  &testValue2,
				"test-key1": &testValue,
			},
		},
		{
			name:    "case9: Old tags are nil and new tags are empty",
			oldTags: nil,
			newTags: map[string]*string{},
			equal:   true,
			merged:  map[string]*string{},
		},
		{
			name:    "case10: Old tags are empty and new tags are nil",
			oldTags: map[string]*string{},
			newTags: nil,
			equal:   true,
			merged:  map[string]*string{},
		},
	}

	for _, tc := range testCases {

		t.Run(tc.name, func(t *testing.T) {
			equal := resourceGroupTagsEqual(tc.oldTags, tc.newTags)
			if equal != tc.equal {
				t.Fatalf("expected %v, got %v", tc.equal, equal)
			}

			merged := mergeResourceTags(tc.oldTags, tc.newTags)
			if len(merged) != len(tc.merged) {
				t.Fatalf("expected %v, got %v", tc.merged, merged)
			}

			for key, value := range tc.merged {
				if *merged[key] != *value {
					t.Fatalf("expected %v, got %v", *value, *merged[key])
				}
			}
		})
	}
}

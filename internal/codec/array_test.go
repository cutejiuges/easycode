package codec

import "testing"

func TestCountArrayElementsBeforeTypedAllocation(t *testing.T) {
	for _, test := range []struct {
		input       string
		limit, want int
		valid       bool
	}{
		{`[]`, 0, 0, true}, {`[{"x":[1,2]},"[,]",null]`, 3, 3, true}, {`[1,2]`, 1, 0, false}, {`{}`, 3, 0, false}, {`[1,`, 3, 0, false}, {`[1] {}`, 3, 0, false}, {`[1]`, -1, 0, false},
	} {
		got, err := CountArrayElements([]byte(test.input), test.limit)
		if (err == nil) != test.valid || err == nil && got != test.want {
			t.Fatalf("input %s: count=%d err=%v", test.input, got, err)
		}
	}
}

package codec

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestScannedObjectRangesAndOwnership(t *testing.T) {
	data := []byte(` {"\u0078": {"nested": [1, "a\\\"b", null]}, "a\/b": -1.25e+3} `)
	before := bytes.Clone(data)
	got, err := ObjectMembers(data)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]json.RawMessage
	if err := json.Unmarshal(data, &want); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("field ranges differ: %s, %v", data, err)
	}
	got["x"][0] = '['
	if !bytes.Equal(data, before) {
		t.Fatal("field result aliases input")
	}
	for i := range data {
		data[i] = 'x'
	}
	if string(got["a/b"]) != "-1.25e+3" {
		t.Fatal("input aliases field result")
	}
}

func TestScannedDepthNullAndNumberBoundaries(t *testing.T) {
	for _, test := range []struct {
		data       string
		depth      int
		rejectNull bool
		valid      bool
	}{
		{`{"items":[{}]}`, 3, false, true},
		{`{"items":[{}]}`, 2, false, false},
		{`{"items":[null]}`, 2, true, false},
		{`{"items":[null]}`, 2, false, true},
		{`"null"`, 1, true, true},
		{`[-0,1.25e+3,1e9999]`, 1, false, true},
		{`[01]`, 1, false, false},
		{`{"x":1,"\u0078":2}`, 1, false, false},
		{`{"x":{"a":1,"a":2}}`, 2, false, false},
		{`{"x":"\ud800"}`, 1, false, false},
		{`{"x":"\ud83d\ude00"}`, 1, false, true},
		{strings.Repeat("[", 2048) + "0" + strings.Repeat("]", 2048), 2048, false, true},
		{strings.Repeat("[", 10001) + "0" + strings.Repeat("]", 10001), 10001, false, true},
		{strings.Repeat("[", 10001) + "0" + strings.Repeat("]", 10001), 10000, false, false},
	} {
		err := ValidateBoundedJSON([]byte(test.data), len(test.data), test.depth, test.rejectNull)
		if (err == nil) != test.valid {
			t.Fatalf("depth=%d rejectNull=%v valid=%v: %v", test.depth, test.rejectNull, test.valid, err)
		}
	}
}

func TestValidationDoesNotMaterializeArrayValues(t *testing.T) {
	measure := func(elements int) float64 {
		data := []byte("[" + strings.Repeat("0,", elements-1) + "0]")
		return testing.AllocsPerRun(20, func() {
			if err := ValidateJSON(data); err != nil {
				t.Fatal(err)
			}
		})
	}
	if small, large := measure(1), measure(1024); large > small+5 {
		t.Fatalf("validation materialized scalar values: small=%v large=%v", small, large)
	}
}

func TestRecordKeepsUnknownFieldPrecedenceOverMissingFields(t *testing.T) {
	var record struct {
		Required string `json:"required"`
	}
	err := UnmarshalRecord([]byte(`{"unknown":true}`), &record)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field diagnosis changed: %v", err)
	}
	err = UnmarshalRecord([]byte(`{}`), &record)
	if err == nil || !strings.Contains(err.Error(), "required JSON field") {
		t.Fatalf("missing field diagnosis changed: %v", err)
	}
}

// FuzzStrictJSONScanner 用独立的逐 token 参考实现检查快速范围扫描的拒绝集合。
func FuzzStrictJSONScanner(f *testing.F) {
	for _, data := range []string{
		`{}`, `[]`, `null`, `true`, `-1.25e+3`, `1e9999`, ` {"x":[{}, "a\\\"b", null]} `,
		`{"x":1,"\u0078":2}`, `{"x":{"a":1,"a":2}}`, `"\ud800"`, `"\ud83d\ude00"`,
		`"\\ud800"`, `{"\uD83D\uDE00":"\/"}`, `{} []`, `[{]`, `"\x00"`, string([]byte{'"', 0xff, '"'}),
		strings.Repeat("[", 10001) + "0" + strings.Repeat("]", 10001),
	} {
		f.Add([]byte(data))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64<<10 {
			return
		}
		want := referenceStrictJSON(data) == nil
		if got := ValidateJSON(data) == nil; got != want {
			t.Fatalf("validation differs for %q: got %v want %v", data, got, want)
		}
		if !want || len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' {
			return
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		got, err := ObjectMembers(data)
		if err != nil || !reflect.DeepEqual(got, fields) {
			t.Fatalf("field ranges differ for %q: %v", data, err)
		}
	})
}

func referenceStrictJSON(data []byte) error {
	if !utf8.Valid(data) || !referenceLosslessUnicode(data) {
		return errors.New("invalid Unicode")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := referenceJSONValue(d); err != nil {
		return err
	}
	var tail json.RawMessage
	if err := d.Decode(&tail); err != io.EOF {
		return errors.New("trailing content")
	}
	return nil
}

func referenceJSONValue(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for d.More() {
			key, err := d.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return errors.New("duplicate or invalid key")
			}
			seen[name] = true
			if err := referenceJSONValue(d); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := referenceJSONValue(d); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid delimiter")
	}
	_, err = d.Token()
	return err
}

// referenceLosslessUnicode 保留逐字节参考算法，独立检查快速路径跳过转义的行为。
func referenceLosslessUnicode(data []byte) bool {
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) {
			return false
		}
		if data[i] != 'u' {
			continue
		}
		if i+4 >= len(data) {
			return false
		}
		n, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n < 0xd800 || n > 0xdbff {
			continue
		}
		if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
			return false
		}
		low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}

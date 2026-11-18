package codec

import (
	"bytes"
	"sync"
	"testing"
)

func TestConcurrentCodecCallsOwnTheirResults(t *testing.T) {
	type record struct {
		Name string `json:"name"`
		Tags []int  `json:"tags"`
	}
	input := []byte(`{"name":"中文","tags":[1,2]}`)
	canonical, err := ParseCanonical(input, 128)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for j := 0; j < 16; j++ {
				var value record
				if err := UnmarshalRecord(input, &value); err != nil {
					t.Error(err)
					return
				}
				encoded, err := MarshalStable(value)
				if err != nil || !bytes.Equal(encoded, input) {
					t.Errorf("concurrent round trip changed bytes: %s, %v", encoded, err)
					return
				}
				fields, err := ObjectMembers(input)
				if err != nil || string(fields["name"]) != `"中文"` {
					t.Errorf("concurrent object fields changed: %v", err)
					return
				}
				// 写入返回副本，其他 worker 同时读取原对象，检查调用间所有权。
				fields["name"][0] = '!'
				encoded[0] = '!'
				value.Tags[0] = -1
				copy := canonical.Bytes()
				copy[0] = '!'
				if !bytes.Equal(canonical.Clone().Bytes(), input) {
					t.Error("canonical bytes alias a caller-owned result")
					return
				}
			}
		}()
	}
	close(start)
	workers.Wait()
	if string(input) != `{"name":"中文","tags":[1,2]}` {
		t.Fatal("shared input changed")
	}
}

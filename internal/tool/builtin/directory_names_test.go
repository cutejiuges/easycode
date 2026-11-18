package builtin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"testing"
)

type batchedNamesFixture struct {
	names               []string
	at, maxBatch, calls int
	cancel              context.CancelFunc
}

func (d *batchedNamesFixture) readEntryNames(batch int) ([]string, error) {
	d.calls++
	d.maxBatch = max(d.maxBatch, batch)
	if d.cancel != nil {
		d.cancel()
	}
	if d.at == len(d.names) {
		return nil, io.EOF
	}
	end := min(len(d.names), d.at+batch)
	names := d.names[d.at:end]
	d.at = end
	return names, nil
}
func (*batchedNamesFixture) openChild(string, workspaceHooks) (workspaceChild, error) {
	return nil, errors.New("not used")
}
func (*batchedNamesFixture) close() error     { return nil }
func (*batchedNamesFixture) interrupt() error { return nil }

func TestWalkerBatchesAndRetainsSameLexicalPrefix(t *testing.T) {
	names := make([]string, 3000)
	for i := range names {
		names[i] = fmt.Sprintf("%05d", len(names)-i)
	}
	expected := append([]string(nil), names...)
	sort.Strings(expected)
	directory := &batchedNamesFixture{names: names}
	result, err := collectWalkerNames(context.Background(), directory, 1001)
	if err != nil || !reflect.DeepEqual(result, expected[:1001]) || directory.maxBatch > 256 || directory.calls < 12 {
		t.Fatalf("bounded enumeration: %v %+v", err, directory)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancelled := &batchedNamesFixture{names: names, cancel: cancel}
	if result, err := collectWalkerNames(ctx, cancelled, 1001); !errors.Is(err, context.Canceled) || result != nil || cancelled.calls != 1 {
		t.Fatal("cancelled walk returned partial names", err)
	}
}

package session

import (
	"bytes"
	"context"
	"os"
	"testing"

	"easycode/internal/codec"
)

func TestInvalidNestedPayloadFailsBeforeTailRepair(t *testing.T) {
	original := mustDecodedRecord(t, EventSampleUsage, testSampleUsage(t))
	original.Sequence = 1
	original.BatchID = 1
	original.BatchIndex = 0
	original.BatchSize = 1
	for _, replacement := range []string{`"state":null`, `"State":"known"`, `"state":"known","state":"unknown"`} {
		record := original
		record.Payload = bytes.Replace(record.Payload, []byte(`"state":"known"`), []byte(replacement), 1)
		if bytes.Equal(record.Payload, original.Payload) {
			t.Fatal("fixture has no known usage")
		}
		record.Checksum = ""
		checksum, err := recordChecksum(record)
		if err != nil {
			t.Fatal(err)
		}
		record.Checksum = checksum
		encoded, err := codec.MarshalStable(record)
		if err != nil {
			t.Fatal(err)
		}
		content := append(encoded, []byte("\n{\"partial\"")...)
		loader, repository := writeRawJournal(t, content)
		_, loadErr := loader.Load(context.Background(), testThreadID)
		if loadErr == nil {
			t.Fatalf("accepted %s", replacement)
		}
		path, err := repository.JournalPath(testThreadID)
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, content) {
			t.Fatal("invalid payload allowed journal repair")
		}
		_ = loader.lease.Close()
		_ = repository.Close()
	}
}

package tool

import "testing"

func TestReadExecutionValidatesCompleteObservationBinding(t *testing.T) {
	id, _ := GenerateInvocationID()
	input, _ := NewReadInput("a", 1, 2000)
	call, _ := NewReadReadyCall("read-call", input)
	v, _ := NewReadInvocation(id, call)
	result := RenderReadSuccess(v, "a", []string{"data"}, 1)
	evidence, _ := NewFileBaseEvidence(NewContentDigest([]byte("data")), 4, 0o600, 1, 2)
	o, err := NewCompleteReadObservation(result, evidence)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewReadExecution(result, o); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"path", "identity", "offset", "eof", "long", "budget", "error"} {
		t.Run(change, func(t *testing.T) {
			bad := result
			switch change {
			case "path":
				bad.readMetadata.relativePath = "b"
			case "identity":
				bad.invocationID, _ = GenerateInvocationID()
			case "offset":
				bad.readMetadata.requestedOffset = 2
			case "eof":
				bad.readMetadata.reachedEOF = false
			case "long":
				bad.readMetadata.longLineTruncated = true
			case "budget":
				bad.readMetadata.outputTruncated = true
			case "error":
				bad.status = ResultError
			}
			if _, err := NewReadExecution(bad, o); err == nil {
				t.Fatal("接受了不匹配证据")
			}
		})
	}
	o = UnavailableReadObservation()
	o.evidence = evidence
	if o.Validate() == nil {
		t.Fatal("不可用观察包含证据")
	}
}

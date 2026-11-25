package app

import (
	"bytes"
	"io"
	"net/http"
	"sync"
	"testing"
)

// requestRecorder 只记录请求和到达顺序，响应与业务断言由各场景负责。
type requestRecorder struct {
	mu     sync.Mutex
	bodies [][]byte
}

func (recorder *requestRecorder) record(t *testing.T, request *http.Request) (int, []byte) {
	t.Helper()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Error(err)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	index := len(recorder.bodies)
	recorder.bodies = append(recorder.bodies, body)
	return index, body
}

func (recorder *requestRecorder) snapshot() [][]byte {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	result := make([][]byte, len(recorder.bodies))
	for index, body := range recorder.bodies {
		result[index] = bytes.Clone(body)
	}
	return result
}

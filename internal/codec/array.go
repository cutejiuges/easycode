package codec

import (
	"bytes"
	"encoding/json"
	"errors"
)

// CountArrayElements 在物化 typed 数组前检查元素上限，每次只暂存一个原生片段。
func CountArrayElements(data []byte, limit int) (int, error) {
	if limit < 0 {
		return 0, errors.New("JSON array limit is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('[') {
		return 0, errors.New("JSON array is required")
	}
	count := 0
	for decoder.More() {
		if count >= limit {
			return 0, errors.New("JSON array exceeds element limit")
		}
		var part json.RawMessage
		if err := decoder.Decode(&part); err != nil {
			return 0, err
		}
		count++
	}
	if _, err := decoder.Token(); err != nil {
		return 0, err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return 0, err
	}
	return count, nil
}

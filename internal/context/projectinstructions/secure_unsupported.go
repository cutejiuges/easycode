//go:build !darwin && !linux

package projectinstructions

import "fmt"

func loadHierarchy(
	string,
	int,
	loaderHooks,
) (map[int]discoveredDocument, int, bool, error) {
	return nil, 0, false, fmt.Errorf("secure project instruction discovery is unsupported on this platform")
}

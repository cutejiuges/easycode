//go:build !darwin && !linux

package skill

import "os"

func openSecure(string, bool, func(string)) (*os.File, error) {
	return nil, failure("skill_platform_unsupported")
}

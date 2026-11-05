//go:build darwin || linux

package skill

import (
	"context"
	"easycode/internal/extension/localfile"
	"os"
)

// openSecure 从根描述符逐段打开，任何项目组件都不跟随链接。
func openSecure(p string, directory bool, hook func(string)) (*os.File, error) {
	return localfile.Open(context.Background(), p, directory, localfile.Options{BeforeOpen: hook})
}

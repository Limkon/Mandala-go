//go:build tools

package tools

// 显式引用 gomobile bind 包，防止 go mod tidy 把它删掉
import (
	_ "golang.org/x/mobile/bind"
	_ "golang.org/x/mobile/cmd/gobind"
	_ "golang.org/x/mobile/cmd/gomobile"
)

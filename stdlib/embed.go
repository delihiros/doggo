// Package stdlib は Doggo の標準ライブラリを提供します。
// doggo/*.dog ファイルはバイナリに埋め込まれ、require 時にフォールバックとして使用されます。
package stdlib

import (
	"embed"
)

// StdlibFS は stdlib/doggo/*.dog を埋め込んだファイルシステムです。
// require 時にディスク上で見つからない場合のフォールバックとして使用されます。
//
//go:embed doggo/*.dog
var StdlibFS embed.FS


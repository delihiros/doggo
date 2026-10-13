// Package reify はDoggo関数からGoインターフェースの実装を作成します。
// このパッケージのコードはcmd/reify_genツールによって自動生成されます。
package reify

//go:generate go run ../../cmd/reify_gen/main.go -o generated_reify.go fmt.Stringer net/http.Handler

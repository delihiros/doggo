// Package opcode はDoggo VMのバイトコード命令を定義します。
package opcode

import (
	"encoding/binary"
	"fmt"
)

// Opcode はバイトコード命令の種類を表します。
type Opcode byte

// 命令定数
const (
	// スタック操作
	OpConst Opcode = 0x00 // 定数プールから値をプッシュ
	OpPop   Opcode = 0x01 // スタックトップを破棄
	OpDup   Opcode = 0x02 // スタックトップを複製

	// リテラル
	OpTrue  Opcode = 0x03 // true をプッシュ
	OpFalse Opcode = 0x04 // false をプッシュ
	OpNil   Opcode = 0x05 // nil をプッシュ

	// 算術演算
	OpAdd Opcode = 0x10 // 加算
	OpSub Opcode = 0x11 // 減算
	OpMul Opcode = 0x12 // 乗算
	OpDiv Opcode = 0x13 // 除算
	OpMod Opcode = 0x14 // 剰余
	OpNeg Opcode = 0x15 // 単項マイナス

	// 比較演算
	OpEq    Opcode = 0x20 // ==
	OpNotEq Opcode = 0x21 // !=
	OpGT    Opcode = 0x22 // >
	OpGTE   Opcode = 0x23 // >=
	OpLT    Opcode = 0x24 // <
	OpLTE   Opcode = 0x25 // <=

	// 論理演算
	OpNot Opcode = 0x28 // 論理否定

	// 制御フロー
	OpJump          Opcode = 0x30 // 無条件ジャンプ
	OpJumpNotTruthy Opcode = 0x31 // 偽ならジャンプ
	OpJumpTruthy    Opcode = 0x32 // 真ならジャンプ

	// 変数アクセス
	OpGetLocal     Opcode = 0x40 // ローカル変数取得
	OpSetLocal     Opcode = 0x41 // ローカル変数設定
	OpGetGlobal    Opcode = 0x42 // グローバル変数取得（Varの場合は値を取得）
	OpSetGlobal    Opcode = 0x43 // グローバル変数設定
	OpGetFree      Opcode = 0x44 // 自由変数取得
	OpBox          Opcode = 0x45 // スタックトップをCellに包む（letfn用）
	OpGetGlobalRaw Opcode = 0x46 // グローバル変数のVar自体を取得（#'用）
	OpDefVar       Opcode = 0x47 // Var定義（def用、メタデータ付き）
	OpInitLocal    Opcode = 0x48 // 新しいローカル束縛を設定（既存のCellを置換）

	// 関数
	OpCall        Opcode = 0x50 // 関数呼び出し
	OpReturnValue Opcode = 0x51 // 値を返す
	OpReturn      Opcode = 0x52 // nilを返す
	OpClosure     Opcode = 0x53 // クロージャ作成

	// コレクション
	OpArray Opcode = 0x60 // ベクター作成
	OpHash  Opcode = 0x61 // マップ作成
	OpIndex Opcode = 0x62 // インデックスアクセス
	OpSet   Opcode = 0x63 // セット作成

	// 組み込み関数
	OpGetBuiltin Opcode = 0x70 // 組み込み関数取得

	// Go相互運用
	OpImport Opcode = 0x71 // Goパッケージのインポート
	OpDot    Opcode = 0x72 // メソッド呼び出し/フィールドアクセス
	OpNew    Opcode = 0x73 // Goオブジェクト生成

	// 並行処理
	OpGo Opcode = 0x74 // ゴルーチン起動（スタック上のクロージャを新ゴルーチンで実行）

	// Goインターフェース実装
	OpReify Opcode = 0x75 // Goインターフェース実装の作成

	// STM（Software Transactional Memory）
	OpDosync Opcode = 0x76 // トランザクションブロック実行（スタック上のクロージャをトランザクション内で実行）

	// 例外処理
	OpThrow  Opcode = 0x80 // 例外送出
	OpTry    Opcode = 0x81 // try ブロック開始 (catch オフセット)
	OpEndTry Opcode = 0x82 // try ブロック正常終了
)

// Definition は命令の定義を保持します。
type Definition struct {
	Name          string // 命令名
	OperandWidths []int  // 各オペランドのバイト幅
}

// definitions は各Opcodeの定義を保持します。
var definitions = map[Opcode]*Definition{
	// スタック操作
	OpConst: {"OpConst", []int{2}}, // uint16: 定数プールインデックス
	OpPop:   {"OpPop", []int{}},
	OpDup:   {"OpDup", []int{}},

	// リテラル
	OpTrue:  {"OpTrue", []int{}},
	OpFalse: {"OpFalse", []int{}},
	OpNil:   {"OpNil", []int{}},

	// 算術演算
	OpAdd: {"OpAdd", []int{}},
	OpSub: {"OpSub", []int{}},
	OpMul: {"OpMul", []int{}},
	OpDiv: {"OpDiv", []int{}},
	OpMod: {"OpMod", []int{}},
	OpNeg: {"OpNeg", []int{}},

	// 比較演算
	OpEq:    {"OpEq", []int{}},
	OpNotEq: {"OpNotEq", []int{}},
	OpGT:    {"OpGT", []int{}},
	OpGTE:   {"OpGTE", []int{}},
	OpLT:    {"OpLT", []int{}},
	OpLTE:   {"OpLTE", []int{}},

	// 論理演算
	OpNot: {"OpNot", []int{}},

	// 制御フロー
	OpJump:          {"OpJump", []int{2}},          // uint16: ジャンプ先
	OpJumpNotTruthy: {"OpJumpNotTruthy", []int{2}}, // uint16: ジャンプ先
	OpJumpTruthy:    {"OpJumpTruthy", []int{2}},    // uint16: ジャンプ先

	// 変数アクセス
	OpGetLocal:     {"OpGetLocal", []int{1}},     // uint8: ローカル変数インデックス
	OpSetLocal:     {"OpSetLocal", []int{1}},     // uint8: ローカル変数インデックス
	OpGetGlobal:    {"OpGetGlobal", []int{2}},    // uint16: グローバル変数インデックス（Varの場合は値を取得）
	OpSetGlobal:    {"OpSetGlobal", []int{2}},    // uint16: グローバル変数インデックス
	OpGetFree:      {"OpGetFree", []int{1}},      // uint8: 自由変数インデックス
	OpBox:          {"OpBox", []int{}},           // スタックトップをCellに包む
	OpGetGlobalRaw: {"OpGetGlobalRaw", []int{2}}, // uint16: グローバル変数のVar自体を取得（#'用）
	OpDefVar:       {"OpDefVar", []int{2, 2}},    // uint16: グローバル変数インデックス, uint16: メタデータ定数インデックス (0=なし)
	OpInitLocal:    {"OpInitLocal", []int{1}},    // uint8: ローカル変数インデックス

	// 関数
	OpCall:        {"OpCall", []int{1}}, // uint8: 引数の数
	OpReturnValue: {"OpReturnValue", []int{}},
	OpReturn:      {"OpReturn", []int{}},
	OpClosure:     {"OpClosure", []int{2, 1}}, // uint16: 定数インデックス, uint8: 自由変数の数

	// コレクション
	OpArray: {"OpArray", []int{2}}, // uint16: 要素数
	OpHash:  {"OpHash", []int{2}},  // uint16: ペア数 * 2
	OpIndex: {"OpIndex", []int{}},
	OpSet:   {"OpSet", []int{2}}, // uint16: 要素数

	// 組み込み関数
	OpGetBuiltin: {"OpGetBuiltin", []int{2}}, // uint16: 組み込み関数インデックス

	// Go相互運用
	OpImport: {"OpImport", []int{2}}, // uint16: パッケージ名（定数プールインデックス）
	OpDot:    {"OpDot", []int{2, 1}}, // uint16: メソッド名（定数プール）, uint8: 引数の数
	OpNew:    {"OpNew", []int{2, 1}}, // uint16: 型名（定数プール）, uint8: 引数の数

	// 並行処理
	OpGo: {"OpGo", []int{}}, // スタック上のクロージャを新ゴルーチンで実行

	// Go インターフェース実装
	OpReify: {"OpReify", []int{}},

	// STM（Software Transactional Memory）
	OpDosync: {"OpDosync", []int{}}, // スタック上のクロージャをトランザクション内で実行

	// 例外処理
	OpThrow:  {"OpThrow", []int{}},
	OpTry:    {"OpTry", []int{2}}, // uint16: catch ブロックへのオフセット
	OpEndTry: {"OpEndTry", []int{}},
}

// Lookup は指定されたOpcodeの定義を返します。
func Lookup(op Opcode) (*Definition, error) {
	def, ok := definitions[op]
	if !ok {
		return nil, fmt.Errorf("opcode %d undefined", op)
	}
	return def, nil
}

// EncodeInstruction は指定されたOpcodeとオペランドをバイトコード命令へエンコードします。
func EncodeInstruction(op Opcode, operands ...int) []byte {
	def, ok := definitions[op]
	if !ok {
		return []byte{}
	}

	// 命令全体の長さを計算
	instructionLen := 1 // opcode自体は1バイト
	for _, w := range def.OperandWidths {
		instructionLen += w
	}

	instruction := make([]byte, instructionLen)
	instruction[0] = byte(op)

	offset := 1
	for i, o := range operands {
		if i >= len(def.OperandWidths) {
			break
		}
		width := def.OperandWidths[i]
		switch width {
		case 1:
			instruction[offset] = byte(o)
		case 2:
			binary.BigEndian.PutUint16(instruction[offset:], uint16(o))
		}
		offset += width
	}

	return instruction
}

// ReadOperands は命令列からオペランドを読み取ります。
// 戻り値: オペランドの配列と読み取ったバイト数
func ReadOperands(def *Definition, ins []byte) ([]int, int) {
	operands := make([]int, len(def.OperandWidths))
	offset := 0

	for i, width := range def.OperandWidths {
		switch width {
		case 1:
			operands[i] = int(ins[offset])
		case 2:
			operands[i] = int(binary.BigEndian.Uint16(ins[offset:]))
		}
		offset += width
	}

	return operands, offset
}

// ReadUint16 はバイト列からBig-Endianのuint16を読み取ります。
func ReadUint16(ins []byte) uint16 {
	return binary.BigEndian.Uint16(ins)
}

// ReadUint8 はバイト列からuint8を読み取ります。
func ReadUint8(ins []byte) uint8 {
	return ins[0]
}

// String はOpcodeの文字列表現を返します。
func (op Opcode) String() string {
	def, ok := definitions[op]
	if !ok {
		return fmt.Sprintf("UNKNOWN(0x%02X)", byte(op))
	}
	return def.Name
}

// Instructions はバイトコード命令列を表します。
type Instructions []byte

// String は命令列を人間が読める形式で返します（ディスアセンブル）。
func (ins Instructions) String() string {
	var out string

	i := 0
	for i < len(ins) {
		def, err := Lookup(Opcode(ins[i]))
		if err != nil {
			out += fmt.Sprintf("ERROR: %s\n", err)
			i++
			continue
		}

		operands, read := ReadOperands(def, ins[i+1:])
		out += fmt.Sprintf("%04d %s\n", i, ins.fmtInstruction(def, operands))
		i += 1 + read
	}

	return out
}

// fmtInstruction は単一の命令を整形して返します。
func (ins Instructions) fmtInstruction(def *Definition, operands []int) string {
	operandCount := len(def.OperandWidths)
	if len(operands) != operandCount {
		return fmt.Sprintf("ERROR: operand len %d does not match defined %d\n",
			len(operands), operandCount)
	}

	switch operandCount {
	case 0:
		return def.Name
	case 1:
		return fmt.Sprintf("%s %d", def.Name, operands[0])
	case 2:
		return fmt.Sprintf("%s %d %d", def.Name, operands[0], operands[1])
	}

	return fmt.Sprintf("ERROR: unhandled operandCount for %s\n", def.Name)
}

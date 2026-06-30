package main

// argScan は各サブコマンドの引数解析の共通形を集約する薄いスキャナ。
// これまで各コマンドに散らばっていた次の 2 つを 1 箇所に閉じ込める:
//
//   - 値を取るフラグの「欠落チェック → 次要素を消費」(value)。
//   - Unix 標準の `--` (オプション終端): 以降はフラグとして解釈せず、すべて位置引数として
//     扱う (token が吸収する)。`-` 始まりの project/id 等を渡したいときの逃げ道になる。
//
// 使い方 (フラグのみのコマンド):
//
//	s := newArgScan(args)
//	for {
//		a, ok := s.token()
//		if !ok {
//			break
//		}
//		switch a {
//		case "--project":
//			v, err := s.value("--project")
//			if err != nil {
//				return err
//			}
//			filterProject = v
//		default:
//			return usagef("unknown option: %s", a)
//		}
//	}
//
// 位置引数を取るコマンドは default で s.positional(a) を呼び、最後に s.rest() で受け取る
// (`--` 以降の語も同じ rest に入る)。
type argScan struct {
	args     []string
	i        int
	pos      []string
	termSeen bool
}

func newArgScan(args []string) *argScan { return &argScan{args: args} }

// token は次のフラグ/語を返す。`--` を見たら以降をすべて位置引数として吸収し、フラグとしては
// 返さない (オプション終端)。これ以上読むものが無ければ ok=false。
func (s *argScan) token() (string, bool) {
	for s.i < len(s.args) {
		a := s.args[s.i]
		s.i++
		switch {
		case s.termSeen:
			s.pos = append(s.pos, a)
		case a == "--":
			s.termSeen = true
		default:
			return a, true
		}
	}
	return "", false
}

// value は直前に token() が返したフラグ name の値 (次の生要素) を返し、消費する。無ければ
// usage エラー。option-argument は getopt 同様、たとえ "--" でも値として取る。
func (s *argScan) value(name string) (string, error) {
	if s.i >= len(s.args) {
		return "", usagef("%s requires a value", name)
	}
	v := s.args[s.i]
	s.i++
	return v, nil
}

// peek は次の生要素を消費せずに覗く。「次が条件を満たすときだけ取る」任意引数
// (例: --recent の N) 用。終端で ok=false。
func (s *argScan) peek() (string, bool) {
	if s.i >= len(s.args) {
		return "", false
	}
	return s.args[s.i], true
}

// skip は peek で覗いた要素を 1 つ消費する (取ると決めたとき)。
func (s *argScan) skip() { s.i++ }

// positional は token() が返した非フラグ語を位置引数として記録する。
func (s *argScan) positional(a string) { s.pos = append(s.pos, a) }

// rest は蓄積した位置引数 (default で積んだ分 + `--` 以降) を返す。
func (s *argScan) rest() []string { return s.pos }

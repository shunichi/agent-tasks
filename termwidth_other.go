//go:build !unix

package main

// ttyWidth は非 unix プラットフォーム向けのフォールバック。ioctl が無いので常に 0 を返し、
// 呼び出し側は COLUMNS 環境変数フォールバック→truncate しない、に落ちる。
func ttyWidth(fd uintptr) int { return 0 }

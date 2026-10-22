// この模擬CLIは受入確認専用で、製品から起動された場合に検査を失敗させる。
package main

import "os"

func main() {
	if err := os.WriteFile(os.Getenv("ENVFACTS_FIXTURE_MARKER"), []byte("模擬CLIが実行されました。\n"), 0o600); err != nil {
		os.Exit(2)
	}
	// 起動失敗を無視して収集を続ける回帰でも、印は必ず残す。
	os.Exit(9)
}

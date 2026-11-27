package main

import (
	"fmt"
	"golang.design/x/clipboard"
)

// Prompt
func prompt(content string) {
	fmt.Println(content)
	err := clipboard.Init()
	if err == nil {
		clipboard.Write(clipboard.FmtText, []byte(content))
	}

}

// readClipboard returns the current clipboard text.
func readClipboard() (string, error) {
	if err := clipboard.Init(); err != nil {
		return "", err
	}
	return string(clipboard.Read(clipboard.FmtText)), nil
}

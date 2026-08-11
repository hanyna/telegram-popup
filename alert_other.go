//go:build !windows

package main

import "fmt"

func alert(title, text string) {
	fmt.Printf("[%s] %s\n", title, text)
}

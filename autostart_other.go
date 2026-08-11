//go:build !windows

package main

import "errors"

func AutostartEnabled() bool     { return false }
func SetAutostart(on bool) error { return errors.New("נתמך רק בווינדוס") }

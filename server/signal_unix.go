//go:build darwin || linux

package main

import (
	"os"
	"os/signal"
)

func signalNotifyUnix(ch chan<- os.Signal, signals ...os.Signal) {
	signal.Notify(ch, signals...)
}

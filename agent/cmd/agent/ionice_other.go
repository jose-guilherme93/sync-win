//go:build !amd64 && !arm64 && !386 && !arm && !riscv64

package main

const ioprioSetNr = 0
const ioniceSupported = false

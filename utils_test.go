package main

import (
	"antinat/utils"
	"fmt"
	"testing"
)

func TestMain(m *testing.M) {
	m.Run()
}

func TestIp2Bytes(t *testing.T) {
	str := "123.234.23.4:10086"
	buf, err := utils.AddrString2bytes(str)
	if err != nil {
		t.Error("AddrString2bytes error")
	}
	fmt.Printf("buf: %v\n", buf)
}

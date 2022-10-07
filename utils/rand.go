package utils

import (
	"math/rand"
	"time"
)

// var bytes []byte = []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz1234567890")

func init() {
	// 保证每次生成的随机数不一样
	rand.Seed(time.Now().UnixNano())
}

// 随机字符串
func RandomString(n int) string {
	result := make([]byte, n)
	rand.Read(result)
	return string(result)
}

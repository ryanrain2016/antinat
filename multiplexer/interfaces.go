package multiplexer

import (
	"net"
	"time"
)

type MultiplexerManager interface {
	GetMultiplexer(remoteName string, bufferSize int) (Multiplexer, error)
	AddMultiplexer(remoteName string, multiplexer Multiplexer) error
	Close() error
}

type Multiplexer interface {
	// 向 sessionId 会话 对应的channel 发送信息
	Write(sessionId uint32, b []byte) (n int, err error)

	// 手动注册channel到multiplexer
	AddChannel(sessionId uint32, ch Channel) error
	// 创建一个channel
	GetChannel(net.Conn) (ch Channel, err error)

	SendConnect(sessionId uint32, addr string) error
	SendClose(sessionId uint32) error
	SendHandShake() error

	StartHeartBeat()

	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	SetDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	Close() error

	ReadOneMessage() (uint32, byte, []byte, error)
	Poll() (err error)

	IsValid() bool
}

type Channel interface {
	Connect(addr string) error
	Write(b []byte) (n int, err error)
	WriteToLocal(b []byte) (n int, err error)
	Close() error
	Poll() (err error)
}

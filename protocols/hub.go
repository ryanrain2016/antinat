package protocols

import (
	"antinat/config"
	"antinat/log"
	"fmt"
	"io"
	"net"
)

type MessageHandler interface {
	OnMessage([]byte) error
	Close() error
}

type Handler struct {
	conn net.Conn
}

func (h *Handler) Handle(mh MessageHandler) {
	defer func() {
		if e := recover(); e != nil {
			err := e.(error)
			log.Error("unexpect error: %s", err.Error())
		}
	}()
	defer func() {
		mh.Close()
	}()
	var err error
	for {
		buf := make([]byte, 2)
		if _, err = io.ReadFull(h.conn, buf); err != nil {
			break
		}
		size := (int(buf[0]) << 8) + int(buf[1])
		buf = make([]byte, size)
		if _, err = io.ReadFull(h.conn, buf); err != nil {
			break
		}
		if err = mh.OnMessage(buf); err != nil {
			log.Error("handle message error: %s", err.Error())
		}
	}
}

func (h *Handler) Write(data []byte) error {
	size := len(data)
	buf := make([]byte, 0)
	buf = append(buf, byte((size&0xff00)>>8))
	buf = append(buf, byte(size&0xff))
	buf = append(buf, data...)
	_, err := h.conn.Write(buf)
	return err
}

type HubProtocol struct {
	Handler
	listeners []net.Listener
	cfg       *config.Config
	hub       *Hub
}

func NewHubProtocol(conn net.Conn, cfg *config.Config, hub *Hub) *HubProtocol {
	hp := new(HubProtocol)
	hp.conn = conn
	hp.listeners = make([]net.Listener, 0)
	hp.cfg = cfg
	hp.hub = hub
	return hp
}

func (hp *HubProtocol) GetConn() net.Conn {
	return hp.conn
}

func (hp *HubProtocol) Close() error {
	errors := make([]error, 0)
	errors = append(errors, hp.conn.Close())
	for _, l := range hp.listeners {
		errors = append(errors, l.Close())
	}
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}

func (hp *HubProtocol) OnMessage(msg []byte) error {
	switch msg[0] {
	case 0x01: // 心跳包
		return hp.onHeartBeat(msg[1:])
	case 0x02: // 注册包
		return hp.onRegister(msg[1:])
	case 0x03: // 连接请求包
		return hp.onConnection(msg[1:])
	case 0x0a: // 控制台消息
		return hp.onConsole(msg[1:])
	default:
		return fmt.Errorf("unsupport message type: %d", msg[0])
	}
}

func (hp *HubProtocol) onHeartBeat(data []byte) error {
	buf := make([]byte, 0)
	buf = append(buf, 0x11)
	buf = append(buf, data...)
	return hp.Write(buf)
}

func (hp *HubProtocol) onRegister(data []byte) error {
	usernameLen := int(data[0])
	username := string(data[1 : 1+usernameLen])
	passLen := int(data[1+usernameLen])
	password := string(data[2+usernameLen : 2+usernameLen+passLen])
	buf := []byte{0x12}
	if hp.cfg.CheckUser(username, password) {
		hp.hub.Gm.Put(username, hp.conn.RemoteAddr().String())
		buf = append(buf, 1)
	} else {
		buf = append(buf, 0)
	}
	return hp.Write(buf)
}

func (hp *HubProtocol) onConnection(data []byte) (err error) {

	return nil
}

func (hp *HubProtocol) onConsole(data []byte) (err error) {
	return nil
}

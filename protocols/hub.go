package protocols

import (
	"antinat/config"
	"antinat/log"
	"antinat/utils"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/pkg/errors"
)

type MessageHandler interface {
	OnMessage([]byte) error
	Close() error
}

type Handler struct {
	conn net.Conn
	loop bool
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
	for h.loop {
		buf, err := h.ReadOneMessage()
		if err != nil {
			if h.loop {
				log.Error("read message from <%s> error: %s", h.conn.RemoteAddr(), err)
			}
			return
		}
		if err = mh.OnMessage(buf); err != nil {
			log.Error("handle message from <%s> error: %+v", h.conn.RemoteAddr(), err)
		}
	}
}

func (h *Handler) ReadOneMessage() (buf []byte, err error) {
	h.conn.SetReadDeadline(time.Now().Add(time.Minute))
	buf = make([]byte, 2)
	log.Trace("start read message length")
	if _, err = io.ReadFull(h.conn, buf); err != nil {
		// err = errors.WithStack(err)
		return
	}
	size := (int(buf[0]) << 8) + int(buf[1])
	log.Trace("read message length: %d", size)
	buf = make([]byte, size)
	log.Trace("start read message")
	if _, err = io.ReadFull(h.conn, buf); err != nil && err != io.EOF {
		log.Trace("read mesage from %s error: %s", h.conn.RemoteAddr(), err.Error())
		// err = errors.WithStack(err)
		return
	}
	log.Trace("Read one message:%v", buf)
	return buf, nil
}

func (h *Handler) Write(data []byte) error {
	size := len(data)
	buf := make([]byte, 0)
	buf = append(buf, byte((size&0xff00)>>8))
	buf = append(buf, byte(size&0xff))
	buf = append(buf, data...)
	_, err := h.conn.Write(buf)
	return errors.WithStack(err)
}

type HubProtocol struct {
	Handler
	listeners []net.Listener
	cfg       *config.Config
	hub       *Hub
	username  string
}

func NewHubProtocol(conn net.Conn, cfg *config.Config, hub *Hub) *HubProtocol {
	hp := new(HubProtocol)
	hp.conn = conn
	hp.listeners = make([]net.Listener, 0)
	hp.cfg = cfg
	hp.hub = hub
	hp.loop = true
	return hp
}

func (hp *HubProtocol) GetConn() net.Conn {
	return hp.conn
}

func (hp *HubProtocol) Close() error {
	if !hp.loop {
		return nil
	}
	hp.loop = false
	log.Debug("<%s> close from <%s>", hp.cfg.GetInstanceName(), hp.conn.RemoteAddr())
	errors := make([]error, 0)
	errors = append(errors, hp.conn.Close())
	for _, l := range hp.listeners {
		errors = append(errors, l.Close())
	}
	hp1 := hp.hub.Gm.Get(hp.username)
	if hp1 == hp {
		hp.hub.Gm.Pop(hp.username)
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
	case 0x13: // 连接响应包
		return hp.onConnectionResponse(msg[1:])
	case 0x0a: // 控制台消息
		return hp.onConsole(msg[1:])
	case 0x09: // 打洞的 忽略
		return nil
	default:
		return fmt.Errorf("<%s>unsupport message type: %d", hp.cfg.GetInstanceName(), msg[0])
	}
}

func (hp *HubProtocol) RequestRegister() error {
	err := hp.Write([]byte{0xff})
	return err
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
	hp.username = username
	if hp.cfg.CheckUser(username, password) {
		hp1 := hp.hub.Gm.Pop(username) // clear earlier register info
		if hp1 != nil && hp1 != hp {
			hp1.(*HubProtocol).Close()
		}
		hp.hub.Gm.Put(username, hp)
		buf = append(buf, 1) // 1 success
		log.Debug("<%s> <%s> register success from <%s>",
			hp.cfg.GetInstanceName(), username, hp.conn.RemoteAddr())
	} else {
		buf = append(buf, 0) // 0 failed
		log.Debug("<%s> <%s> register failed from <%s>",
			hp.cfg.GetInstanceName(), username, hp.conn.RemoteAddr())
	}
	return hp.Write(buf)
}

func (hp *HubProtocol) onConnection(data []byte) (err error) {
	// defer func() { hp.loop = false }() // 新建的连接请求的连接，不走消息循环
	// 这里不能关闭，不然发送响应消息时 连接已关闭
	username, data, err := utils.ParseString(data)
	if err != nil {
		return errors.WithStack(err)
	}
	password, data, err := utils.ParseString(data)
	if err != nil {
		return errors.WithStack(err)
	}
	if !hp.cfg.CheckUser(username, password) {
		hp.Write([]byte("\x13\x00"))
		return errors.WithStack(errors.New("connection auth failed"))
	}
	node, data, err := utils.ParseString(data)
	if err != nil {
		return errors.WithStack(err)
	}
	// portBytes := data[:2]
	// rport, data, err := utils.ParsePort(data)
	if err != nil {
		return errors.WithStack(err)
	}
	log.Info("<%s> connection from <%s> to <%s>",
		hp.cfg.GetInstanceName(),
		hp.conn.RemoteAddr().String(),
		node,
		// rport,
	)
	remoteHpI := hp.hub.Gm.Get(node)
	if remoteHpI == nil {
		hp.Write([]byte("\x13\x00"))
		return errors.WithStack(fmt.Errorf("connect to <%s> failed, it's offline", node))
	}
	remoteHp := remoteHpI.(*HubProtocol)
	key := hp.hub.Gm.PutWithRandomKey(hp)
	addr := hp.conn.RemoteAddr().(*net.UDPAddr)
	buf := append([]byte{0x03}, []byte(key)...) // request connect
	ipBytes, _ := utils.IP2Bytes(addr.IP)
	buf = append(buf, ipBytes...)
	buf = append(buf, utils.Port2Bytes(addr.Port)...)
	// buf = append(buf, portBytes...)
	buf = append(buf, data...)
	remoteHp.Write(buf)
	return nil
}

func (hp *HubProtocol) onConnectionResponse(data []byte) (err error) {
	// data[0] == 0 failed； data[0] == 1 success
	// data[1:11] key
	// if success; get hp.conn.RemoteAddr send to request node
	// else send failed to request node
	log.Debug("<%s> read a conncetion response from <%s>",
		hp.cfg.GetInstanceName(),
		hp.conn.RemoteAddr())
	key := string(data[1:11])
	hp1, ok := hp.hub.Gm.Pop(key).(*HubProtocol)
	if !ok || hp1 == nil {
		log.Error("<%s> read connection response error, invalid key", hp.cfg.GetInstanceName())
		return errors.WithStack(errors.New("invalid connection response"))
	}
	log.Debug("<%s> find request conncetion from %s",
		hp.cfg.GetInstanceName(),
		hp1.conn.RemoteAddr())
	defer func() {
		time.Sleep(time.Second * 2)
		hp1.Close()
	}()
	if data[0] == 0 {
		hp1.Write([]byte{0x13, 0x00})
		log.Debug("<%s> <%s> said connect failed",
			hp.cfg.GetInstanceName(),
			hp.conn.RemoteAddr())
		return nil
	}
	log.Debug("<%s> read a success connection response from %s",
		hp.cfg.GetInstanceName(),
		hp.conn.RemoteAddr())
	buf := []byte{0x13, 0x01}
	remoteAddr, _ := hp.conn.RemoteAddr().(*net.UDPAddr)
	ipBytes, _ := utils.IP2Bytes(remoteAddr.IP)
	buf = append(buf, ipBytes...)
	buf = append(buf, utils.Port2Bytes(remoteAddr.Port)...)
	log.Debug("<%s> write a success connection response to %s",
		hp.cfg.GetInstanceName(),
		hp1.conn.RemoteAddr())
	time.Sleep(time.Millisecond * 500)
	return errors.WithStack(hp1.Write(buf))
}

func (hp *HubProtocol) onConsole(data []byte) (err error) {
	return nil
}

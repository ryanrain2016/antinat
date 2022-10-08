package protocols

import (
	"antinat/config"
	"antinat/log"
	"fmt"
	"net"
	"time"

	"github.com/pkg/errors"
)

type NodeProtocol struct {
	Handler
	cfg             *config.Config
	connections     []net.Conn
	heartbeat       byte
	heartbeatTicker *time.Ticker
	heartbeatStop   chan int
	node            *Node
}

func NewNodeProtocol(conn net.Conn, cfg *config.Config, node *Node) *NodeProtocol {
	np := new(NodeProtocol)
	np.cfg = cfg
	np.conn = conn
	np.connections = make([]net.Conn, 0)
	np.heartbeat = 0
	np.heartbeatTicker = time.NewTicker(time.Second * 30)
	np.heartbeatStop = make(chan int)
	np.node = node
	np.loop = true
	return np
}

func (np *NodeProtocol) Close() error {
	np.StopHeartBeat()
	errors := make([]error, 0)
	errors = append(errors, np.conn.Close())
	for _, l := range np.connections {
		errors = append(errors, l.Close())
	}
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}

func (np *NodeProtocol) OnMessage(msg []byte) error {
	switch msg[0] {
	case 0x11: // 心跳响应包
		return np.onHeartBeatResponse(msg[1:])
	case 0x12: // 注册响应包
		return np.onRegisterResponse(msg[1:])
	case 0x03: // 连接请求包
		return np.onConnection(msg[1:])
	// case 0x13: // 连接请求响应包
	// 	return np.onConnectionResponse(msg[1:])
	case 0x0a: // 控制台消息
		return np.onControl(msg[1:])
	default:
		return fmt.Errorf("unsupport message type: %d", msg[0])
	}
}

func (np *NodeProtocol) onRegisterResponse(buf []byte) error {
	if buf[0] == 1 {
		log.Info("<%s> register successfully", np.cfg.GetInstanceName())
		return nil
	}
	panic(ErrREGISTER_FAILED)
}

func (np *NodeProtocol) onConnection(buf []byte) (err error) {
	// create a new connection to hub, to make hole to request addr

	return nil
}

func (np *NodeProtocol) onConnectionResponse(buf []byte) (raddr string, err error) {
	if buf[0] != 1 {
		return "", errors.WithStack(errors.New("connection error"))
	}
	return "", nil
}

func (np *NodeProtocol) onControl(data []byte) (err error) {
	return nil
}

func (np *NodeProtocol) Register() error {
	auth := np.cfg.GetAuth()
	if auth == nil {
		return fmt.Errorf("<%s> has no auth config", np.cfg.GetInstanceName())
	}
	authBytes := auth.ToBytes()
	authBytes = append([]byte{0x02}, authBytes...)
	err := np.Write(authBytes)
	return err
}

func (np *NodeProtocol) HeartBeat() {
	np.heartbeat++
	buf := append([]byte{0x01}, np.heartbeat)
	np.Write(buf)
}

func (np *NodeProtocol) onHeartBeatResponse(buf []byte) error {
	if buf[0] == np.heartbeat {
		return nil
	}
	return fmt.Errorf("wrong heartbeat read, perhaps the connection delay is to heavy")
}

func (np *NodeProtocol) StartHeartBeat() {
	for {
		select {
		case <-np.heartbeatTicker.C:
			go np.HeartBeat()
		case <-np.heartbeatStop:
			return
		}
	}
}

func (np *NodeProtocol) StopHeartBeat() {
	np.heartbeatStop <- 1
	np.heartbeatTicker.Stop()
}

func (np *NodeProtocol) Connect(nodeName string, port int) (net.Conn, error) {
	_, conn, err := np.cfg.CreateConnectionToHub()
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer func() { conn.Close() }()
	np1 := NewNodeProtocol(conn, np.cfg, np.node)
	auth := np.cfg.GetAuth() // if auth is nil, panic occurs when register
	authBytes := auth.ToBytes()
	connBytes := append([]byte{0x03}, authBytes...)
	connBytes = append(connBytes, byte(len(nodeName)))
	connBytes = append(connBytes, []byte(nodeName)...)
	connBytes = append(connBytes, byte((port&0xff00)>>8), byte(port&0xff))
	if err = np1.Write(connBytes); err != nil {
		return nil, errors.WithStack(err)
	}
	buf, err := np1.ReadOneMessage()
	if err != nil {
		return nil, errors.WithStack(err)
	}
	if buf[0] != 0x13 {
		return nil, errors.WithStack(errors.New("expect connection response unexpect connection"))
	}
	raddr, err := np1.onConnectionResponse(buf[1:])
	if err != nil {
		return nil, errors.WithStack(err)
	}
	laddr := conn.LocalAddr()
	conn.Close()
	conn, err = np1.cfg.CreateKcpConnection(raddr, laddr)
	return conn, err
}

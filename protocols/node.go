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
	np.heartbeatStop = make(chan int, 1)
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

func (np *NodeProtocol) onConnectionFailed(key []byte) (err error) {
	connectResponseBytes := append([]byte{0x13, 0x00}, key...)
	return np.Write(connectResponseBytes)
}

func (np *NodeProtocol) onConnection(buf []byte) (err error) {
	// buf: key: 10byte, remote_ip(5 or 17 byte), remote_port 2 byte, local_port 2 byte
	// create a new connection to hub , to make hole to request addr, then tell hub completemake hole
	key := buf[:10]
	raddr := new(net.UDPAddr)
	raddr.IP, buf, err = utils.ParseIP(buf[10:])
	if err != nil {
		log.Error("parse IP error, when connect: %s", err.Error())
		np.onConnectionFailed(key)
		return errors.WithStack(err)
	}
	raddr.Port, buf, err = utils.ParsePort(buf)
	if err != nil {
		log.Error("parse port error, when connect: %s", err.Error())
		np.onConnectionFailed(key)
		return errors.WithStack(err)
	}
	localPort, _, err := utils.ParsePort(buf)
	// raddr.IP = net.IP(buf[10:14])
	// raddr.Port = (int(buf[14]) << 8) | int(buf[15])
	// localPort := (int(buf[16]) << 8) | int(buf[17])
	udp, conn, err := np.cfg.CreateConnectionToHub()
	if err != nil {
		log.Error("connect to hub error, when connect: %s", err.Error())
		np.onConnectionFailed(key)
		return errors.WithStack(err)
	}
	defer func() { conn.Close() }()
	localAddr := conn.LocalAddr().String()
	lAddr := fmt.Sprintf("127.0.0.1:%d", localPort)
	log.Debug("connecting to local<%s>", lAddr)
	lConn, err := net.Dial("tcp", lAddr)
	if err != nil {
		log.Debug("connect to local %s , when connect: %s", lAddr, err.Error())
		np.onConnectionFailed(key)
		return errors.WithStack(err)
	}
	log.Debug("connect to local<%s> done, start to make hole to %s", lAddr, raddr)
	for i := 0; i <= 10; i++ { // make hole
		udp.WriteToUDP([]byte("\x0f\xff"), raddr)
	}
	connectResponseBytes := append([]byte{0x13, 0x01}, key...)
	np1 := NewNodeProtocol(conn, np.cfg, np.node)
	defer np1.Close()
	log.Debug("node write a success connection response")
	err = np1.Write(connectResponseBytes)
	if err != nil {
		log.Error("send to connect response error")
		lConn.Close()
		return errors.WithStack(err)
	}
	log.Debug("node write response done")
	go func() {
		defer func() {
			if e := recover(); e != nil {
				err := e.(error)
				log.Error("unexpect error: %+v", err)
			}
		}()
		defer lConn.Close()
		log.Debug("start listen on %s...", localAddr)
		np1.Close()
		listener, err := np.cfg.CreateListener(localAddr)
		if err != nil {
			panic(err)
		}
		defer listener.Close()
		conn, err := listener.Accept()
		if err != nil {
			panic(err)
		}
		defer conn.Close()
		go io.Copy(conn, lConn)
		io.Copy(lConn, conn)
	}()
	return nil
}

func (np *NodeProtocol) onConnectionResponse(buf []byte) (raddr string, err error) {
	// buf[0] == 1 success
	// buf[0] == 0 failed
	// when success buf[1:]: addr
	if buf[0] != 1 {
		return "", errors.WithStack(errors.New("connection error"))
	}
	// ip := net.IP(buf[1:5]).String()
	// port := (int(buf[6]) << 8) | int(buf[7])
	ip, buf, err := utils.ParseIP(buf[1:])
	if err != nil {
		return "", errors.WithStack(err)
	}
	port, _, err := utils.ParsePort(buf)
	if err != nil {
		return "", errors.WithStack(err)
	}
	return fmt.Sprintf("%s:%d", ip, port), nil
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
	defer func() {
		_ = recover()
	}()
	np.heartbeatTicker.Stop()
	np.heartbeatStop <- 1
	close(np.heartbeatStop)
}

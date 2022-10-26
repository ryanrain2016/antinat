package protocols

import (
	"antinat/config"
	"antinat/log"
	"antinat/utils"
	"fmt"
	"io"
	"math/rand"
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
	heartbeatChan   chan byte
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
	np.heartbeatChan = make(chan byte, 1)
	np.node = node
	np.loop = true
	return np
}

func (np *NodeProtocol) Close() error {
	if !np.loop {
		return nil
	}
	np.loop = false
	log.Debug("<%s> <%s> close connection to hub", np.cfg.GetInstanceName(), np.conn.LocalAddr())
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
	case 0xff: //注册请求包
		return np.Register()
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
		return fmt.Errorf("<%s>unsupport message type: %d",
			np.cfg.GetInstanceName(),
			msg[0])
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
		log.Error("<%s> parse IP error, when connect: %s",
			np.cfg.GetInstanceName(),
			err.Error())
		np.onConnectionFailed(key)
		return errors.WithStack(err)
	}
	raddr.Port, buf, err = utils.ParsePort(buf)
	if err != nil {
		log.Error("<%s> parse port error, when connect: %s",
			np.cfg.GetInstanceName(),
			err.Error())
		np.onConnectionFailed(key)
		return errors.WithStack(err)
	}
	localPort, buf, err := utils.ParsePort(buf)
	// raddr.IP = net.IP(buf[10:14])
	// raddr.Port = (int(buf[14]) << 8) | int(buf[15])
	// localPort := (int(buf[16]) << 8) | int(buf[17])
	udp, conn, err := np.cfg.CreateConnectionToHub()
	if err != nil {
		log.Error("<%s> connect to hub error, when connect: %s",
			np.cfg.GetInstanceName(),
			err.Error())
		np.onConnectionFailed(key)
		return errors.WithStack(err)
	}
	defer func() { conn.Close() }()
	localAddr := conn.LocalAddr().String()
	lAddr := fmt.Sprintf("127.0.0.1:%d", localPort)
	log.Debug("<%s> connecting to local<%s>",
		np.cfg.GetInstanceName(),
		lAddr)
	lConn, err := net.Dial("tcp", lAddr)
	if err != nil {
		log.Debug("<%s> connect to local %s , when connect: %s",
			np.cfg.GetInstanceName(),
			lAddr, err.Error())
		np.onConnectionFailed(key)
		return errors.WithStack(err)
	}
	log.Debug("<%s> connect to local<%s> done, start to make hole to %s",
		np.cfg.GetInstanceName(),
		lAddr, raddr)

	np.MakeHole(udp, raddr, buf)
	connectResponseBytes := append([]byte{0x13, 0x01}, key...)
	np1 := NewNodeProtocol(conn, np.cfg, np.node)
	log.Debug("<%s> node write a success connection response", np.cfg.GetInstanceName())
	err = np1.Write(connectResponseBytes)
	if err != nil {
		log.Error("<%s> send to connect response error", np.cfg.GetInstanceName())
		lConn.Close()
		np1.Close()
		return errors.WithStack(err)
	}
	log.Debug("<%s> node write response done", np.cfg.GetInstanceName())
	go func() {
		defer func() {
			if e := recover(); e != nil {
				err := e.(error)
				log.Error("<%s> unexpect error when accept connection from other node: %+v",
					np.cfg.GetInstanceName(),
					err)
			}
		}()
		defer lConn.Close()
		log.Debug("<%s> start listen on %s...",
			np.cfg.GetInstanceName(),
			localAddr)
		np1.Close()
		listener, err := np.cfg.CreateListener(localAddr)
		if err != nil {
			panic(err)
		}
		defer listener.Close()
		var conn net.Conn
		for {
			conn, err = listener.Accept()
			if err != nil {
				panic(err)
			}
			remoteAddr := conn.RemoteAddr().String()
			log.Debug("<%s>get a connection from %s, expect %s",
				np.cfg.GetInstanceName(),
				remoteAddr, raddr)
			hubAddr, _ := np.cfg.GetHubAddr()
			if remoteAddr == hubAddr {
				conn.Close()
				continue
			} else {
				break
			}
		}
		log.Debug("<%s>accept a connection from %s",
			np.cfg.GetInstanceName(),
			conn.RemoteAddr().String())
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

func (np *NodeProtocol) HeartBeat(b byte) {
	buf := append([]byte{0x01}, b)
	np.Write(buf)
	t := time.NewTimer(time.Minute)
	select {
	case <-t.C:
		log.Error("<%s>heartbeat timeout, closing", np.cfg.GetInstanceName())
		np.Close()
	case <-np.heartbeatChan:
		t.Stop()
	}
}

func (np *NodeProtocol) onHeartBeatResponse(buf []byte) error {
	np.heartbeatChan <- buf[0]
	if buf[0] == np.heartbeat {
		return nil
	}
	np.Close()
	return fmt.Errorf("<%s>wrong heartbeat read, closing", np.cfg.GetInstanceName())
}

func (np *NodeProtocol) StartHeartBeat() {
	np.heartbeat++
	go np.HeartBeat(np.heartbeat)
	for {
		select {
		case <-np.heartbeatTicker.C:
			np.heartbeat++
			go np.HeartBeat(np.heartbeat)
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

func (np *NodeProtocol) MakeHole(udp *net.UDPConn, raddr *net.UDPAddr, ips []byte) {
	makehole := func(udp *net.UDPConn, raddr *net.UDPAddr) {
		for j := 0; j < 50; j++ {
			n := rand.Intn(20) + 1
			result := make([]byte, n)
			rand.Read(result)
			udp.WriteToUDP(result, raddr)
		}
	}
	go makehole(udp, raddr)
	addr := &net.UDPAddr{
		Port: raddr.Port,
	}
	ipString := raddr.IP.String()
	var err error
	var ip net.IP
	for len(ips) > 0 {
		ip, ips, err = utils.ParseIP(ips)
		if err != nil {
			break
		}
		if ip.String() == ipString {
			continue
		}
		addr.IP = ip
		makehole(udp, addr)
	}
}

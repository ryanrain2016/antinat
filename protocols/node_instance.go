package protocols

import (
	"antinat/config"
	"antinat/log"
	"antinat/multiplexer"
	"antinat/utils"
	"fmt"
	"net"
	"time"

	"github.com/pkg/errors"
)

type Node struct {
	cfg       *config.Config
	conn      net.Conn
	Gm        *GlobalMap
	listeners []net.Listener

	MultiplexerManager multiplexer.MultiplexerManager
}

func NewNode(cfg *config.Config) (n *Node, err error) {
	n = new(Node)
	n.cfg = cfg
	_, conn, err := cfg.CreateConnectionToHub()
	if err != nil {
		return nil, err
	}
	n.conn = conn
	n.listeners = make([]net.Listener, 0)
	n.Gm = &GlobalMap{
		lock:  make(chan int, 1),
		inner: make(map[string]interface{}),
	}
	n.MultiplexerManager = multiplexer.NewMultiplexerManager(cfg.GetInstanceName(), n.ConnectionFactory)
	return
}

func (n *Node) Run() {
	var err error
	defer func() {
		if e := recover(); e != nil {
			err = e.(error)
			log.Error("<%s> server run error: %s",
				n.cfg.GetInstanceName(),
				err.Error())
		}
	}()
	defer n.Close()
	np := NewNodeProtocol(n.conn, n.cfg, n)
	go np.StartHeartBeat()
	// go np.Register()
	go n.HandlePortMap()
	np.Handle(np)
}

func (n *Node) ConnectionFactory(remoteName string) (net.Conn, error) {
	log.Debug("<%s> create a new connetion to hub", n.cfg.GetInstanceName())
	_, conn, err := n.cfg.CreateConnectionToHub()
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer func() { conn.Close() }()
	np := NewNodeProtocol(conn, n.cfg, n)
	// create connect bytes
	auth := n.cfg.GetAuth()
	authBytes := auth.ToBytes()
	connBytes := append([]byte{0x03}, authBytes...)
	nodeBytes, err := utils.String2Bytes(remoteName)
	if err != nil {
		return nil, errors.WithStack(fmt.Errorf("node name is to long: %s", remoteName))
	}
	connBytes = append(connBytes, nodeBytes...)
	ips := n.cfg.GetExternalIps()
	for _, ip := range ips {
		ipBytes, _ := utils.IP2Bytes(ip)
		connBytes = append(connBytes, ipBytes...)
	}
	log.Debug("<%s> write connect request to hub", n.cfg.GetInstanceName())
	if err = np.Write(connBytes); err != nil {
		log.Error("<%s> write connction request error", n.cfg.GetInstanceName())
		return nil, errors.WithStack(err)
	}
	var buf []byte
	for {
		buf, err = np.ReadOneMessage()
		if err != nil { // 这里是超时错误
			log.Error("<%s> read conncetion response error",
				n.cfg.GetInstanceName())
			return nil, errors.WithStack(err)
		}
		if buf[0] == 0x13 { // 这里收到响应信息跳出
			break
		}
		log.Debug("<%s> read a message, type is %d",
			n.cfg.GetInstanceName(),
			buf[0])
	}
	log.Debug("<%s> read a connection response", n.cfg.GetInstanceName())
	raddr, err := np.onConnectionResponse(buf[1:])
	if err != nil {
		log.Error("<%s> parse connection response error", n.cfg.GetInstanceName())
		return nil, errors.WithStack(err)
	}
	log.Debug("<%s> the opposite node is behind <%s>", n.cfg.GetInstanceName(), raddr)
	laddr := conn.LocalAddr()
	np.Close()
	log.Debug("<%s> start to connect to remote %s from %s", n.cfg.GetInstanceName(), raddr, laddr)
	_, newConn, err := n.cfg.CreateKcpConnection(raddr, laddr)
	if err != nil {
		log.Debug("<%s> connect failed to remote failed", n.cfg.GetInstanceName())
		return nil, errors.WithStack(err)
	}
	log.Debug("<%s> connected to remote %s from %s",
		n.cfg.GetInstanceName(),
		newConn.RemoteAddr(),
		newConn.LocalAddr())
	buf = []byte{0xff}
	newConn.Write(buf) // write a byte to communicate
	timeout := n.cfg.GetNodeConnectTimeout()
	newConn.SetReadDeadline(time.Now().Add(time.Second * time.Duration(timeout)))
	_, err = newConn.Read(buf)
	if err != nil {
		log.Error("<%s> handshake byte read timeout", n.cfg.GetInstanceName())
		newConn.Close()
		return nil, err
	}
	newConn.SetReadDeadline(time.Time{})
	return newConn, err
}

func (n *Node) HandlePortMap() {
	maps := n.cfg.GetPortMaps()
	for name, pm := range maps {
		go n.handlePortMap(name, pm)
	}
}

func (n *Node) handlePortMap(name string, pm *config.PortMap) {
	listener, err := net.Listen("tcp", pm.BindAddr)
	if err != nil {
		log.Error("<%s> handling port map listen on %s error: %s",
			n.cfg.GetInstanceName(),
			pm.BindAddr,
			err.Error())
		return
	}
	log.Info("<%s> Listen on %s to redirect to %s:%d",
		n.cfg.GetInstanceName(),
		pm.BindAddr, pm.RemoteNode, pm.RemotePort)
	n.listeners = append(n.listeners, listener)
	defer func() { listener.Close() }()
	for {
		conn, err := listener.Accept()
		if err != nil {
			break
		}
		go func() {
			defer func() { conn.Close() }()
			multiplexer, err := n.MultiplexerManager.GetMultiplexer(pm.RemoteNode, 256)
			if err != nil {
				log.Error("<%s> connect to %s:%d error: %s",
					n.cfg.GetInstanceName(),
					pm.RemoteNode, pm.RemotePort, err.Error())
				return
			}
			ch, _ := multiplexer.GetChannel(conn)
			ch.Connect(fmt.Sprintf("127.0.0.1:%d", pm.RemotePort))
			ch.Poll()
		}()
	}
}

func (n *Node) Close() {
	for _, l := range n.listeners {
		l.Close()
	}
	n.conn.Close()
}

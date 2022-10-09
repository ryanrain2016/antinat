package protocols

import (
	"antinat/config"
	"antinat/log"
	"io"
	"net"

	"github.com/pkg/errors"
)

type Node struct {
	cfg  *config.Config
	conn net.Conn
	Gm   *GlobalMap
}

func NewNode(cfg *config.Config) (n *Node, err error) {
	n = new(Node)
	n.cfg = cfg
	_, conn, err := cfg.CreateConnectionToHub()
	if err != nil {
		return nil, err
	}
	n.conn = conn
	n.Gm = &GlobalMap{
		lock:  make(chan int, 1),
		inner: make(map[string]interface{}),
	}
	return
}

func (n *Node) Run() {
	var err error
	defer func() {
		if e := recover(); e != nil {
			err = e.(error)
			log.Error("server run error: %s", err.Error())
		}
	}()
	np := NewNodeProtocol(n.conn, n.cfg, n)
	go np.StartHeartBeat()
	go np.Register()
	np.Handle(np)
}

func (n *Node) Connect(nodeName string, port int) (net.Conn, error) {
	_, conn, err := n.cfg.CreateConnectionToHub()
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer func() { conn.Close() }()
	np1 := NewNodeProtocol(conn, n.cfg, n)
	auth := n.cfg.GetAuth() // if auth is nil, panic occurs when register
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

func (n *Node) HandlePortMap() {
	maps := n.cfg.GetPortMaps()
	for name, pm := range maps {
		go n.handlePortMap(name, pm)
	}
}

func (n *Node) handlePortMap(name string, pm *config.PortMap) {
	listener, err := net.Listen("tcp", pm.BindAddr)
	if err != nil {
		log.Error("handling port map listen on %s error: %s", pm.BindAddr, err.Error())
	}
	defer func() { listener.Close() }()
	for {
		conn, err := listener.Accept()
		if err != nil {
			break
		}
		go func() {
			defer func() { conn.Close() }()
			rConn, err := n.Connect(pm.RemoteNode, pm.RemotePort)
			if err != nil {
				log.Error("connect to %s:%d error: %s", pm.RemoteNode, pm.RemotePort, err.Error())
				return
			}
			defer func() { rConn.Close() }()
			go io.Copy(conn, rConn)
			io.Copy(rConn, conn)
		}()
	}
}

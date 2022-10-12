package protocols

import (
	"antinat/config"
	"antinat/log"
	"antinat/utils"
	"fmt"
	"io"
	"net"

	"github.com/pkg/errors"
)

type Node struct {
	cfg       *config.Config
	conn      net.Conn
	Gm        *GlobalMap
	listeners []net.Listener
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
	defer n.Close()
	np := NewNodeProtocol(n.conn, n.cfg, n)
	go np.StartHeartBeat()
	// go np.Register()
	go n.HandlePortMap()
	np.Handle(np)
}

func (n *Node) Connect(nodeName string, port int) (net.Conn, error) {
	log.Debug("node create a new connetion to hub")
	_, conn, err := n.cfg.CreateConnectionToHub()
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer func() { conn.Close() }()
	np := NewNodeProtocol(conn, n.cfg, n)
	auth := n.cfg.GetAuth() // if auth is nil, panic occurs when register
	authBytes := auth.ToBytes()
	connBytes := append([]byte{0x03}, authBytes...)
	nodeBytes, err := utils.String2Bytes(nodeName)
	if err != nil {
		return nil, errors.WithStack(fmt.Errorf("node name is to long: %s", nodeName))
	}
	connBytes = append(connBytes, nodeBytes...)
	connBytes = append(connBytes, utils.Port2Bytes(port)...)
	log.Debug("node write connect requets to hub")
	if err = np.Write(connBytes); err != nil {
		log.Error("node write connction request error")
		return nil, errors.WithStack(err)
	}
	var buf []byte
	for {
		buf, err = np.ReadOneMessage()
		if err != nil {
			log.Error("node read conncetion response error")
			return nil, errors.WithStack(err)
		}
		if buf[0] == 0x13 {
			break
		}
	}
	log.Debug("node read a conncetion response")
	raddr, err := np.onConnectionResponse(buf[1:])
	if err != nil {
		log.Error("node parse conncetion response error")
		return nil, errors.WithStack(err)
	}
	log.Debug("the oppsite node is behind <%s>", raddr)
	laddr := conn.LocalAddr()
	np.Close()
	log.Debug("start to connect to remote %s from %s", raddr, laddr)
	_, newConn, err := n.cfg.CreateKcpConnection(raddr, laddr)
	if err != nil {
		log.Debug("connect failed to remote failed")
		return nil, errors.WithStack(err)
	}
	log.Debug("connected to remote %s from %s", newConn.RemoteAddr(), newConn.LocalAddr())
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
		log.Error("handling port map listen on %s error: %s", pm.BindAddr, err.Error())
		return
	}
	log.Info("Listen on %s to redirect to %s:%d", pm.BindAddr, pm.RemoteNode, pm.RemotePort)
	n.listeners = append(n.listeners, listener)
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
			_, err = io.Copy(rConn, conn)
			if err != nil {
				log.Debug("error when pipe: %s", err.Error())
			}
		}()
	}
}

func (n *Node) Close() {
	for _, l := range n.listeners {
		l.Close()
	}
}

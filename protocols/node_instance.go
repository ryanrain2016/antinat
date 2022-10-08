package protocols

import (
	"antinat/config"
	"antinat/log"
	"net"
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

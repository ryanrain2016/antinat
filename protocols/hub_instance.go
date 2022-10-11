package protocols

import (
	"antinat/config"
	"antinat/log"
	"antinat/utils"
	"fmt"
	"net"
)

type Runner interface {
	Run()
}

func NewRunner(cfg *config.Config) (Runner, error) {
	if role, err := cfg.GetRole(); role == "hub" {
		return NewHub(cfg)
	} else if role == "node" {
		return NewNode(cfg)
	} else if err != nil {
		return nil, err
	} else {
		return nil, fmt.Errorf("<%s>'s role [%s] is invalid", cfg.GetInstanceName(), role)
	}
}

type GlobalMap struct {
	lock  chan int
	inner map[string]interface{}
}

func (gm *GlobalMap) Put(key string, i interface{}) {
	gm.lock <- 1
	defer func() {
		<-gm.lock
	}()
	gm.inner[key] = i
}

func (gm *GlobalMap) PutWithRandomKey(i interface{}) (key string) {
	gm.lock <- 1
	defer func() {
		<-gm.lock
	}()
	for {
		key = utils.RandomString(10)
		if _, ok := gm.inner[key]; !ok {
			break
		}
	}
	gm.inner[key] = i
	return
}

func (gm *GlobalMap) Get(key string) interface{} {
	return gm.inner[key]
}

func (gm *GlobalMap) Pop(key string) interface{} {
	i, ok := gm.inner[key]
	if ok {
		gm.lock <- 1
		defer func() {
			<-gm.lock
		}()
		delete(gm.inner, key)
	}
	return i
}

type Hub struct {
	cfg      *config.Config
	listener net.Listener
	Gm       *GlobalMap
}

func NewHub(cfg *config.Config) (*Hub, error) {
	s := new(Hub)
	s.cfg = cfg
	if err := s.listen(); err != nil {
		return nil, err
	}
	s.Gm = &GlobalMap{
		lock:  make(chan int, 1),
		inner: make(map[string]interface{}),
	}
	return s, nil
}

func (h *Hub) listen() (err error) {
	var bind_addr string
	if bind_addr, err = h.cfg.GetBindAddr(); err != nil {
		return
	}
	var listener net.Listener
	listener, err = h.cfg.CreateListener(bind_addr)
	if err == nil {
		log.Info("listening on [%s] ...", bind_addr)
		h.listener = listener
	}
	return err
}

func (h *Hub) Run() {
	var err error
	defer func() {
		if e := recover(); e != nil {
			err = e.(error)
			log.Error("server run error: %s", err.Error())
		}
	}()
	for {
		var conn net.Conn
		if conn, err = h.listener.Accept(); err == nil {
			log.Debug("<%s> connection from <%s>", h.cfg.GetInstanceName(), conn.RemoteAddr())
			conn = h.cfg.WrapHubConn(conn)
			hp := NewHubProtocol(conn, h.cfg, h)
			hp.RequestRegister()
			go hp.Handle(hp)
		}
	}
}

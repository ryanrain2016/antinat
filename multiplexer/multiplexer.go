package multiplexer

import (
	"crypto/rand"
	"io"
	"math/big"
	"net"
	"sync"
	"time"

	"github.com/pkg/errors"
)

type multiplexerManager struct {
	ConnectFactory func(remoteName string) (net.Conn, error) // 调用之后获取一个通向一个节点的链接

	name string // 本地节点名字

	multiplexerMap        map[string]Multiplexer
	multiplexerMapLockers map[string]sync.Locker
	multiplexerMapLock    sync.Locker
}

func NewMultiplexerManager(name string, connectFactory func(remoteName string) (net.Conn, error)) MultiplexerManager {
	return &multiplexerManager{
		ConnectFactory:        connectFactory,
		name:                  name,
		multiplexerMap:        make(map[string]Multiplexer),
		multiplexerMapLockers: make(map[string]sync.Locker),
		multiplexerMapLock:    &sync.Mutex{},
	}
}

func (mm *multiplexerManager) getMultiplexerMapLocker(remoteName string) sync.Locker {
	mm.multiplexerMapLock.Lock()
	defer mm.multiplexerMapLock.Unlock()
	var v sync.Locker
	var ok bool
	if v, ok = mm.multiplexerMapLockers[remoteName]; !ok {
		v = &sync.Mutex{}
		mm.multiplexerMapLockers[remoteName] = v
	}
	return v
}

func (mm *multiplexerManager) AddMultiplexer(remoteName string, multiplexer Multiplexer) error {
	locker := mm.getMultiplexerMapLocker(remoteName)
	locker.Lock()
	defer locker.Unlock()
	if _, ok := mm.multiplexerMap[remoteName]; ok {
		return errors.WithStack(errors.Errorf("multiplexer exists"))
	}
	mm.multiplexerMap[remoteName] = multiplexer
	return nil
}

func (mm *multiplexerManager) GetMultiplexer(remoteName string, bufferSize int) (Multiplexer, error) {
	// 新建或者获取已存在的通向{name}节点的多路复用器
	locker := mm.getMultiplexerMapLocker(remoteName)
	locker.Lock()
	defer locker.Unlock()
	var v Multiplexer
	var ok bool
	if v, ok = mm.multiplexerMap[remoteName]; !ok || !v.IsValid() {
		conn, err := mm.ConnectFactory(remoteName)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		v = NewMultiplexer(conn, mm.name, bufferSize, nil)
		go v.StartHeartBeat()
		go v.SendHandShake()
		go v.Poll()
		mm.multiplexerMap[remoteName] = v
	}
	return v, nil
}

type multiplexer struct {
	conn         net.Conn
	channels     map[uint32]Channel
	channelMutex sync.Locker

	name string // 本地节点名称

	writeMutex   sync.Locker
	writeChannel chan []byte

	// 获取到对方的节点name时调用，主要是要添加到multiplexerManager中
	remoteNameCallback func(remoteName string) error

	heartbeat       byte
	heartbeatTicker *time.Ticker
	heartbeatStop   chan int
	heartbeatChan   chan byte

	loop bool
}

func NewMultiplexer(conn net.Conn, name string, bufferSize int, remoteNameCallback func(remoteName string) error) Multiplexer {
	return &multiplexer{
		conn:         conn,
		channels:     make(map[uint32]Channel),
		channelMutex: &sync.Mutex{},
		name:         name,
		writeMutex:   &sync.Mutex{},
		writeChannel: make(chan []byte, bufferSize),

		remoteNameCallback: remoteNameCallback,

		heartbeatTicker: time.NewTicker(time.Second * 30),
		heartbeatStop:   make(chan int, 1),
		heartbeatChan:   make(chan byte, 1),

		loop: true,
	}
}

// 从 sessionId 会话 对应的channel 发送信息到远方
func (m *multiplexer) Write(sessionId uint32, b []byte) (n int, err error) {
	m.writeMutex.Lock()
	defer m.writeMutex.Unlock()
	for len(b) > 1500 {
		err = m.writeMessage(sessionId, 0x02, b[:1500])
		if err != nil {
			return n, errors.WithStack(err)
		}
		n += 1500
		b = b[1500:]
	}
	err = m.writeMessage(sessionId, 0x02, b)
	if err != nil {
		return n, errors.WithStack(err)
	}
	n += len(b)
	return n, nil
}

func (m *multiplexer) GetChannel(conn net.Conn) (ch Channel, err error) {
	m.channelMutex.Lock()
	defer m.channelMutex.Unlock()
	var sessionId uint32
	for {
		n, _ := rand.Int(rand.Reader, big.NewInt(0xffffffff))
		sessionId = uint32(n.Int64())
		if _, ok := m.channels[sessionId]; !ok {
			break
		}
	}
	ch = &channel{
		conn:         conn,
		sessionId:    sessionId,
		multiplexer:  m,
		writeChannel: make(chan []byte, 256),
		loop:         true,
	}
	m.channels[sessionId] = ch
	return
}

func (m *multiplexer) AddChannel(sessionId uint32, ch Channel) error {
	m.channelMutex.Lock()
	defer m.channelMutex.Unlock()
	if _, ok := m.channels[sessionId]; ok {
		return errors.WithStack(errors.Errorf("session exists: %s", sessionId))
	}
	m.channels[sessionId] = ch
	return nil
}

func (m *multiplexer) LocalAddr() net.Addr {
	return m.conn.LocalAddr()
}

func (m *multiplexer) RemoteAddr() net.Addr {
	return m.conn.RemoteAddr()
}

func (m *multiplexer) SetDeadline(t time.Time) error {
	return errors.WithStack(m.conn.SetDeadline(t))
}

func (m *multiplexer) SetReadDeadline(t time.Time) error {
	return errors.WithStack(m.conn.SetReadDeadline(t))
}

func (m *multiplexer) SetWriteDeadline(t time.Time) error {
	return errors.WithStack(m.conn.SetWriteDeadline(t))
}

func (m *multiplexer) Close() error {
	if m.loop {
		m.loop = false
		close(m.writeChannel)
		m.StopHeartBeat()
		for sessionId := range m.channels {
			m.closeSession(sessionId)
		}
		return errors.WithStack(m.conn.Close())
	}
	return nil
}

func (m *multiplexer) ReadOneMessage() (sessionId uint32, cbyte byte, msg []byte, err error) {
	// 每个消息会增加 7 个字节的消息头
	// 前4个字节是会话ID， 第5个字节为控制字节， 6-7字节，消息的长度
	// 一分钟没有收到心跳就关闭连接了
	m.SetReadDeadline(time.Now().Add(time.Minute))
	buf := make([]byte, 7)
	if _, err = io.ReadFull(m.conn, buf); err != nil {
		err = errors.WithStack(err)
		return
	}
	// fmt.Printf("recv: %v\n", buf)
	sessionId = bytes2SessionId(buf[:4])
	cbyte = buf[4]
	length := bytes2Length(buf[5:7])
	if length != 0 {
		msg = make([]byte, length)
		if _, err = io.ReadFull(m.conn, msg); err != nil {
			err = errors.WithStack(err)
			return
		}
	}
	return
}

func (m *multiplexer) writeMessage(sessionId uint32, cbyte byte, msg []byte) (err error) {
	defer func() {
		e := recover()
		err, _ = e.(error)
	}()
	buf := sessionId2Bytes(sessionId)
	buf = append(buf, cbyte)
	if msg != nil {
		buf = append(buf, length2Bytes(len(msg))...)
		buf = append(buf, msg...)
	} else {
		buf = append(buf, 0x00, 0x00)
	}
	m.writeChannel <- buf
	return
}

func (m *multiplexer) IsValid() bool {
	return m.loop
}

func (m *multiplexer) Poll() error {
	defer m.Close()
	go func() {
		defer func() {
			_ = recover()
		}()
		for m.loop {
			buf, ok := <-m.writeChannel
			if ok {
				// fmt.Println("multiplexer write:", buf)
				m.conn.Write(buf)
			} else {
				// fmt.Println("multiplexer close")
				return
			}
		}
	}()
	for m.loop {
		sessionId, cbyte, msg, err := m.ReadOneMessage()
		if err != nil {
			// fmt.Printf("%v\n", err)
			return errors.WithStack(err)
		}
		if err := m.handleMessage(sessionId, cbyte, msg); err != nil {
			if sessionId == 0 {
				return errors.WithStack(err)
			}
			m.closeSession(sessionId)
		}
	}
	return nil
}

func (m *multiplexer) sendConnectResponse(sessionId uint32, msg byte) error {
	return m.writeMessage(sessionId, 0x11, []byte{msg})
}

func (m *multiplexer) handleMessage(sessionId uint32, cbyte byte, msg []byte) error {
	// fmt.Println("handling", sessionId, cbyte, msg)
	// sessionId 会话ID
	// cbyte 控制字节
	// msg 消息
	switch cbyte {
	case 0x00: // 接受对方传过来的节点名称
		name, _, err := parseString(msg)
		if err != nil {
			return errors.WithStack(err)
		}
		if m.remoteNameCallback != nil {
			err = m.remoteNameCallback(name)
			if err != nil {
				return errors.WithStack(err)
			}
		}
	case 0x01: // 新建连接
		err := m.handleConnect(sessionId, msg)
		if err != nil {
			return errors.WithStack(err)
		}
	case 0x02: // 传输数据
		m.channelMutex.Lock()
		defer m.channelMutex.Unlock()
		ch, ok := m.channels[sessionId]
		if !ok {
			return errors.WithStack(errors.Errorf("unknown sessionId: %d", sessionId))
		}
		_, err := ch.WriteToLocal(msg)
		if err != nil {
			return errors.WithStack(err)
		}
	case 0x03: // 关闭连接
		m.closeSession(sessionId)
	case 0xff: // 心跳
		buf := []byte{0x00, 0x00, 0x00, 0x00, 0xfe, 0x00, 0x01, msg[0]}
		m.conn.Write(buf)
	case 0xfe: // 心跳响应
		m.heartbeatChan <- msg[0]
		if msg[0] != m.heartbeat {
			return errors.WithStack(errors.New("wrong heartbeat byte"))
		}
	case 0x11: // 连接请求响应
		m.channelMutex.Lock()
		defer m.channelMutex.Unlock()
		ch, ok := m.channels[sessionId]
		if !ok {
			// 可能这边关闭了， 告诉对面也关闭
			m.SendClose(sessionId)
			return errors.WithStack(errors.Errorf("unknown sessionId: %d", sessionId))
		}
		if msg[0] == 1 {
			go ch.Poll()
		} else {
			m.closeSession(sessionId)
		}
	}
	return nil
}

func (m *multiplexer) handleConnect(sessionId uint32, msg []byte) error {
	var resp byte = 1
	defer m.sendConnectResponse(sessionId, resp)

	addr, _, err := parseString(msg)
	if err != nil {
		resp = 0
		return errors.WithStack(err)
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		resp = 0
		return errors.WithStack(err)
	}
	ch := &channel{
		conn:         conn,
		sessionId:    sessionId,
		multiplexer:  m,
		writeChannel: make(chan []byte, 256),
		loop:         true,
	}
	if err = m.AddChannel(sessionId, ch); err != nil {
		resp = 0
		return errors.WithStack(err)
	}
	go ch.Poll()
	return nil
}

func (m *multiplexer) closeSession(sessionId uint32) error {
	m.channelMutex.Lock()
	defer m.channelMutex.Unlock()
	ch, ok := m.channels[sessionId]
	if !ok {
		return errors.WithStack(errors.Errorf("unknown sessionId: %d", sessionId))
	}
	delete(m.channels, sessionId)
	return ch.Close()
}

func (m *multiplexer) SendHandShake() error {
	nameBytes := []byte(m.name)
	return m.writeMessage(0, 0x00, append(length2Bytes(len(nameBytes)), nameBytes...))
}

func (m *multiplexer) SendConnect(sessionId uint32, addr string) error {
	addrBytes := []byte(addr)
	return m.writeMessage(sessionId, 0x01, append(length2Bytes(len(addrBytes)), addrBytes...))
}

func (m *multiplexer) SendClose(sessionId uint32) error {
	return m.writeMessage(sessionId, 0x03, []byte{})
}

func (m *multiplexer) heartBeat(b byte) {
	buf := []byte{0x00, 0x00, 0x00, 0x00, 0xff, 0x00, 0x01, b}
	m.conn.Write(buf)
	t := time.NewTimer(time.Minute)
	select {
	case <-t.C:
		m.Close()
	case <-m.heartbeatChan:
		t.Stop()
	}
}

func (m *multiplexer) StartHeartBeat() {
	m.heartbeat++
	go m.heartBeat(m.heartbeat)
	for {
		select {
		case <-m.heartbeatTicker.C:
			m.heartbeat++
			go m.heartBeat(m.heartbeat)
		case <-m.heartbeatStop:
			return
		}
	}
}

func (m *multiplexer) StopHeartBeat() {
	defer func() {
		_ = recover()
	}()
	m.heartbeatTicker.Stop()
	m.heartbeatStop <- 1
	close(m.heartbeatStop)
}

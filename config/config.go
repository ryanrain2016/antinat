package config

import (
	"antinat/log"
	_ "antinat/test"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/Unknwon/goconfig"
	"github.com/pkg/errors"
	"github.com/xtaci/kcp-go"
	"golang.org/x/crypto/pbkdf2"
)

var configFile string
var config *goconfig.ConfigFile

type KcpConfig struct {
	key  string
	salt string
}

func (kc *KcpConfig) GetEncryptBlock() kcp.BlockCrypt {
	if kc.key != "" && kc.salt != "" {
		key := pbkdf2.Key([]byte(kc.key), []byte(kc.salt), 1024, 32, sha1.New)
		block, _ := kcp.NewAESBlockCrypt(key)
		return block
	}
	return nil
}

func init() {
	flag.StringVar(&configFile, "c", "antinat.conf", "配置文件默认为 antinat.conf")
	flag.Parse()
	var err error
	config, err = goconfig.LoadConfigFile(configFile)
	if err != nil {
		panic(fmt.Sprintf("invalid config file: %s", configFile))
	}
	level := GetLogLevel()
	log.InitLog(level)
}

func readFileToBytes(filename string) ([]byte, error) {
	return os.ReadFile(filename)
}

func GetLogLevel() string {
	return config.MustValue("", "log_level", "Trace")
}

func GetInstances() ([]string, error) {
	instanceNames, err := config.GetValue("", "instance")
	if err != nil {
		return nil, err
	}
	instances := regexp.MustCompile(`\s*[,\.\\|:;/]\s*`).Split(instanceNames, -1)
	return instances, nil
}

type Auth struct {
	username string
	password string
}

func (auth *Auth) ToBytes() []byte {
	buf := make([]byte, 0)
	buf = append(buf, byte(len(auth.username)))
	buf = append(buf, []byte(auth.username)...)
	buf = append(buf, byte(len(auth.password)))
	buf = append(buf, []byte(auth.password)...)
	return buf
}

type Config struct {
	instanceName string
	tlsConfig    *tls.Config
	kc           *KcpConfig
	users        map[string]string
}

func NewConfig(instanceName string) *Config {
	var cfg = new(Config)
	cfg.instanceName = instanceName
	if tlsConfig, err := cfg.getTlsConfig(); err != nil {
		log.Warning("connection not encrypt: %s", err)
	} else {
		cfg.tlsConfig = tlsConfig
	}
	cfg.kc = cfg.getKcpConfig()
	if role, _ := cfg.GetRole(); role == "hub" {
		cfg.users = cfg.getUsers()
	}
	return cfg
}

func (cfg *Config) GetInstanceName() string {
	return cfg.instanceName
}

func (cfg *Config) GetRole() (string, error) {
	return config.GetValue(cfg.instanceName, "role")
}

func (cfg *Config) GetBindAddr() (string, error) {
	if role, _ := cfg.GetRole(); role != "hub" {
		return "", errors.New("only hub should config bind_addr")
	}
	return config.GetValue(cfg.instanceName, "bind_addr")
}

func (cfg *Config) GetHubAddr() (string, error) {
	if role, _ := cfg.GetRole(); role != "node" {
		return "", errors.New("only node should config hub_addr")
	}
	return config.GetValue(cfg.instanceName, "hub_addr")
}

func (cfg *Config) getConfigFileBytes(section string, key string) ([]byte, error) {
	if certFile, err := config.GetValue(section, key); err != nil {
		return nil, err
	} else {
		return readFileToBytes(certFile)
	}
}

func (cfg *Config) getSSLServerName() string {
	section := fmt.Sprintf("%s.ssl", cfg.instanceName)
	return config.MustValue(section, "server_name", "abc")
}

func (cfg *Config) GetTlsConfig() *tls.Config {
	return cfg.tlsConfig
}

func (cfg *Config) getTlsConfig() (*tls.Config, error) {
	var role string
	var err error
	if role, err = cfg.GetRole(); err != nil {
		return nil, err
	}
	if role == "hub" {
		return cfg.getHubTlsConfig()
	} else if role == "node" {
		return cfg.getClientTlsConfig()
	} else {
		return nil, errors.New("role is invalid")
	}
}

func (cfg *Config) getHubTlsConfig() (tlsConfig *tls.Config, err error) {
	if role, _ := cfg.GetRole(); role != "hub" {
		return nil, errors.New("only hub can call getHubTlsConfig")
	}
	defer func() {
		if e := recover(); e != nil {
			err = e.(error)
		}
	}()
	certBytes, err := cfg.getConfigFileBytes(
		fmt.Sprintf("%s.ssl", cfg.instanceName),
		"cert",
	)
	if err != nil {
		return nil, err
	}
	keyBytes, err := cfg.getConfigFileBytes(
		fmt.Sprintf("%s.ssl", cfg.instanceName),
		"key",
	)
	if err != nil {
		return
	}
	caBytes, err := cfg.getConfigFileBytes(
		fmt.Sprintf("%s.ssl", cfg.instanceName),
		"ca",
	)
	if err != nil {
		return
	}
	fmt.Printf("certBytes: %v\n", certBytes)
	certCert, err := tls.X509KeyPair(certBytes, keyBytes)
	if err != nil {
		return
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caBytes)
	tlsConfig = &tls.Config{
		Certificates:       []tls.Certificate{certCert},
		ClientCAs:          caCertPool,
		InsecureSkipVerify: false,
		ClientAuth:         tls.RequireAndVerifyClientCert,
	}
	return
}

func (cfg *Config) getClientTlsConfig() (tlsConfig *tls.Config, err error) {
	if role, _ := cfg.GetRole(); role != "node" {
		return nil, errors.New("only node can call getClientTlsConfig")
	}
	defer func() {
		if e := recover(); e != nil {
			err = e.(error)
		}
	}()
	certBytes, err := cfg.getConfigFileBytes(
		fmt.Sprintf("%s.ssl", cfg.GetInstanceName()),
		"cert",
	)
	if err != nil {
		return nil, err
	}
	keyBytes, err := cfg.getConfigFileBytes(
		fmt.Sprintf("%s.ssl", cfg.GetInstanceName()),
		"key",
	)
	if err != nil {
		return
	}
	caBytes, err := cfg.getConfigFileBytes(
		fmt.Sprintf("%s.ssl", cfg.GetInstanceName()),
		"ca",
	)
	if err != nil {
		return
	}
	certCert, err := tls.X509KeyPair(certBytes, keyBytes)
	if err != nil {
		return
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caBytes)
	tlsConfig = &tls.Config{
		Certificates:       []tls.Certificate{certCert},
		RootCAs:            caCertPool,
		InsecureSkipVerify: false,
		ServerName:         cfg.getSSLServerName(),
	}
	return
}

func (cfg *Config) getKcpConfig() *KcpConfig {
	section := fmt.Sprintf("%s.kcp", cfg.instanceName)

	if m, err := config.GetSection(section); err != nil {
		return nil
	} else {
		kc := new(KcpConfig)
		kc.key = m["key"]
		kc.salt = m["salt"]
		return kc
	}
}

func (cfg *Config) GetKcpConfig() *KcpConfig {
	return cfg.kc
}

func (cfg *Config) CreateKcpConnection(raddr string, laddr net.Addr) (udp *net.UDPConn, conn net.Conn, err error) {
	kc := cfg.getKcpConfig()
	if kc == nil {
		return nil, nil, fmt.Errorf("[%s.kcp] config not right", cfg.instanceName)
	}
	addr := laddr.(*net.UDPAddr)
	network := "udp4"
	if addr.IP.To4() == nil {
		network = "udp"
	}
	udp, err = net.ListenUDP(network, addr)
	if err != nil {
		return nil, nil, err
	}
	if block := kc.GetEncryptBlock(); block != nil {
		conn, err = kcp.NewConn(raddr, block, 10, 3, udp)
	} else {
		conn, err = kcp.NewConn(raddr, nil, 0, 0, udp)
	}
	return
}

func (cfg *Config) createKcpListener(laddr net.Addr) (listener net.Listener, err error) {
	kc := cfg.getKcpConfig()
	if kc == nil {
		return nil, fmt.Errorf("[%s.kcp] config not right", cfg.instanceName)
	}
	addr := laddr.(*net.UDPAddr)
	udp, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}
	if block := kc.GetEncryptBlock(); block != nil {
		return kcp.ServeConn(block, 10, 3, udp)
	} else {
		return kcp.ServeConn(nil, 0, 0, udp)
	}
}

func (cfg *Config) CreateConnectionToHub() (udp *net.UDPConn, conn net.Conn, err error) {
	var role string
	if role, _ = cfg.GetRole(); role != "node" {
		return nil, nil, errors.New("role must be node to create connection to server")
	}
	server_addr, _ := cfg.GetHubAddr()
	tlsConfig := cfg.GetTlsConfig()
	kc := cfg.getKcpConfig()

	udpaddr, err := net.ResolveUDPAddr("udp", server_addr)
	if err != nil {
		return nil, nil, err
	}
	network := "udp4"
	if udpaddr.IP.To4() == nil {
		network = "udp"
	}
	udp, err = net.ListenUDP(network, nil)
	if err != nil {
		return nil, nil, err
	}
	if block := kc.GetEncryptBlock(); block != nil {
		conn, err = kcp.NewConn2(udpaddr, block, 10, 3, udp)
	} else {
		conn, err = kcp.NewConn2(udpaddr, nil, 0, 0, udp)
	}
	kcpConn := conn.(*kcp.UDPSession)
	kcpConn.SetStreamMode(true)
	kcpConn.SetACKNoDelay(true)
	kcpConn.SetNoDelay(1, 10, 2, 1)
	if err == nil && tlsConfig != nil {
		conn = tls.Client(conn, tlsConfig)
	}
	return
}

func (cfg *Config) CreateListener(bind_addr string) (listener net.Listener, err error) {
	kc := cfg.getKcpConfig()
	if kc == nil {
		// return net.Listen("tcp", bind_addr)
		return nil, fmt.Errorf("[%s.kcp] config not right", cfg.instanceName)
	} else if block := kc.GetEncryptBlock(); block != nil {
		return kcp.ListenWithOptions(bind_addr, block, 10, 3)
	} else {
		return kcp.Listen(bind_addr)
	}
}

func (cfg *Config) WrapHubConn(conn net.Conn) net.Conn {
	if tlsConfig := cfg.GetTlsConfig(); tlsConfig != nil {
		return tls.Server(conn, tlsConfig)
	}
	return conn
}

func (cfg *Config) getUsers() map[string]string {
	section := fmt.Sprintf("%s.users", cfg.instanceName)
	m, err := config.GetSection(section)
	if err != nil {
		log.Warning("<%s> get users error: %s", cfg.instanceName, err.Error())
		return make(map[string]string)
	}
	return m
}

func (cfg *Config) GetUsers() map[string]string {
	return cfg.users
}

func (cfg *Config) CheckUser(username string, password string) bool {
	users := cfg.GetUsers()
	return users[username] == password
}

func (cfg *Config) GetAuth() *Auth {
	value, err := config.GetValue(cfg.GetInstanceName(), "auth")
	if err != nil {
		return nil
	}
	parts := strings.SplitN(value, ":", 2)
	return &Auth{
		username: parts[0],
		password: parts[1],
	}
}

func (cfg *Config) GetConsoleAddr() {

}

func (cfg *Config) GetPortMaps() (maps map[string]*PortMap) {
	maps = make(map[string]*PortMap)
	section := fmt.Sprintf("%s.map", cfg.instanceName)
	sections, err := config.GetSection(section)
	if err != nil {
		return
	}
	for name, mapString := range sections {
		if pm, err := NewPortMap(name, mapString); err == nil {
			maps[name] = pm
		} else {
			log.Warning("err: %v", err)
		}
	}
	log.Info("port maps: %v", maps)
	return maps
}

type PortMap struct {
	Name       string
	BindAddr   string
	RemoteNode string
	RemoteAddr string
}

func NewPortMap(name string, mapString string) (*PortMap, error) {
	reg := regexp.MustCompile(`((?:(\d+\.\d+\.\d+\.\d+):)?(\d+))\s*[-/;|]\s*([^:]+):(.+)`)
	parts := reg.FindStringSubmatch(mapString)
	if len(parts) == 0 {
		return nil, fmt.Errorf("invalid config[%s], skip", mapString)
	}
	pm := new(PortMap)
	pm.Name = name
	if parts[2] == "" {
		pm.BindAddr = "127.0.0.1:" + parts[3]
	} else {
		pm.BindAddr = parts[1]
	}
	pm.RemoteNode = parts[4]
	if _, err := strconv.Atoi(parts[5]); err != nil {
		pm.RemoteAddr = parts[5]
	} else {
		pm.RemoteAddr = fmt.Sprintf("127.0.0.1:%s", parts[5])
	}
	return pm, nil
}

func (cfg *Config) GetExternalIps() []net.IP {
	str, err := config.GetValue(cfg.instanceName, "external_ips")
	if err != nil {
		return []net.IP{}
	}
	ips := regexp.MustCompile(`\s*[,\\|:;/]\s*`).Split(str, -1)
	IPs := make([]net.IP, 0)
	for _, ip := range ips {
		if IP := net.ParseIP(ip); IP != nil {
			IPs = append(IPs, IP)
		}
	}
	return IPs
}

func (cfg *Config) getInt(section, key string, deft int) int {
	section = fmt.Sprintf("%s.%s", cfg.instanceName, section)
	str, err := config.GetValue(section, key)
	if err != nil {
		return deft
	}
	n, err := strconv.Atoi(str)
	if err != nil {
		return deft
	}
	return n
}

func (cfg *Config) GetMakeholePacketNumber() int {
	return cfg.getInt("connect", "makehole_packet_number", 1)
}

func (cfg *Config) GetMakeholePacketLength() int {
	return cfg.getInt("connect", "makehole_packet_length", 20)
}

func (cfg *Config) GetNodeConnectTimeout() int {
	return cfg.getInt("connect", "connect_timeout", 60)
}

func (cfg *Config) GetNodeListenTimeout() int {
	return cfg.getInt("connect", "listen_timeout", 60)
}

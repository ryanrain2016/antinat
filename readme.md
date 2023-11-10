# 内网端口映射工具
仅用于学习测试之用，内网端口映射是否成功受运行的网络环境的影响

# 功能
P2P技术实现两个局域网之间的内网端口映射，可以将A局域网中的某个端口映射到B局域网中的某台机器上的另一端口
需要1个hub+2个node；hub有公网IP，两个node的公网IP能互通(这个可以用traceroute等工具测试)
连接建立后流量不通过hub，直接在两个node中建立可靠传输连接
通讯使用[kcp](github.com/xtaci/kcp-go)，一种基于udp的可靠传输协议
使用[kcp](github.com/xtaci/kcp-go)自带的加密方式

# TODO
+ 双方通过协商的sessionID建立连接，不用使用CS模式建立kcp连接
+ P2P失败时能通过hub中转
+ 控制台程序

# 原理
见[百度百科](https://baike.baidu.com/item/%E5%86%85%E7%BD%91%E7%A9%BF%E9%80%8F/8597835)实现流程部分
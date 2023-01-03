package config

import (
	"context"
	"encoding/json"
	"github.com/graarh/golang-socketio"
	"github.com/graarh/golang-socketio/transport"
	"golang.org/x/net/websocket"
	"m7s.live/engine/v4/log"
	"m7s.live/engine/v4/util"
	"net/http"
	"strings"
	"time"
)

type PublishConfig interface {
	GetPublishConfig() *Publish
}

type SubscribeConfig interface {
	GetSubscribeConfig() *Subscribe
}
type PullConfig interface {
	GetPullConfig() *Pull
}

type PushConfig interface {
	GetPushConfig() *Push
}

type Publish struct {
	PubAudio          bool
	PubVideo          bool
	KickExist         bool // 是否踢掉已经存在的发布者
	PublishTimeout    int  // 发布无数据超时
	WaitCloseTimeout  int  // 延迟自动关闭（等待重连）
	DelayCloseTimeout int  // 延迟自动关闭（无订阅时）
}

func (c *Publish) GetPublishConfig() *Publish {
	return c
}

type Subscribe struct {
	SubAudio       bool
	SubVideo       bool
	SubAudioTracks []string // 指定订阅的音频轨道
	SubVideoTracks []string // 指定订阅的视频轨道
	LiveMode       bool     // 实时模式：追赶发布者进度，在播放首屏后等待发布者的下一个关键帧，然后调到该帧。
	IFrameOnly     bool     // 只要关键帧
	WaitTimeout    int      // 等待流超时
}

func (c *Subscribe) GetSubscribeConfig() *Subscribe {
	return c
}

type Pull struct {
	RePull          int               // 断开后自动重拉,0 表示不自动重拉，-1 表示无限重拉，高于0 的数代表最大重拉次数
	PullOnStart     bool              // 启动时拉流
	PullOnSubscribe bool              // 订阅时自动拉流
	PullList        map[string]string // 自动拉流列表，以streamPath为key，url为value
}

func (p *Pull) GetPullConfig() *Pull {
	return p
}

func (p *Pull) AddPull(streamPath string, url string) {
	if p.PullList == nil {
		p.PullList = make(map[string]string)
	}
	p.PullList[streamPath] = url
}

type Push struct {
	RePush   int               // 断开后自动重推,0 表示不自动重推，-1 表示无限重推，高于0 的数代表最大重推次数
	PushList map[string]string // 自动推流列表
}

func (p *Push) GetPushConfig() *Push {
	return p
}

func (p *Push) AddPush(url string, streamPath string) {
	if p.PushList == nil {
		p.PushList = make(map[string]string)
	}
	p.PushList[url] = streamPath
}

type Console struct {
	Server        string //远程控制台地址
	Secret        string //远程控制台密钥
	PublicAddr    string //公网地址，提供远程控制台访问的地址，不配置的话使用自动识别的地址
	PublicAddrTLS string
}

type Engine struct {
	Publish
	Subscribe
	HTTP
	RTPReorder     bool
	EnableAVCC     bool //启用AVCC格式，rtmp协议使用
	EnableRTP      bool //启用RTP格式，rtsp、gb18181等协议使用
	EnableSubEvent bool //启用订阅事件,禁用可以提高性能
	EnableAuth     bool //启用鉴权
	Console
	LogLevel            string
	RTPReorderBufferLen int //RTP重排序缓冲长度
	SpeedLimit          int //速度限制最大等待时间
	EventBusSize        int //事件总线大小
}

var Global = &Engine{
	Publish:        Publish{true, true, false, 10, 0, 0},
	Subscribe:      Subscribe{true, true, nil, nil, true, false, 10},
	HTTP:           HTTP{ListenAddr: ":8080", CORS: true, mux: http.DefaultServeMux},
	RTPReorder:     true,
	EnableAVCC:     true,
	EnableRTP:      true,
	EnableSubEvent: true,
	EnableAuth:     true,
	Console: Console{
		"console.monibuca.com:4242", "", "", "",
	},
	LogLevel:            "info",
	RTPReorderBufferLen: 50,
	SpeedLimit:          0,
	EventBusSize:        10,
}

type myResponseWriter struct {
}

func (*myResponseWriter) Header() http.Header {
	return make(http.Header)
}
func (*myResponseWriter) WriteHeader(statusCode int) {
}
func (w *myResponseWriter) Flush() {
}

type myWsWriter struct {
	myResponseWriter
	*websocket.Conn
}

func (w *myWsWriter) Write(b []byte) (int, error) {
	return len(b), websocket.Message.Send(w.Conn, b)
}

func (cfg *Engine) WsRemote() {
	for {
		conn, err := websocket.Dial(cfg.Server, "", "https://console.monibuca.com")
		wr := &myWsWriter{Conn: conn}
		if err != nil {
			log.Error("connect to console server ", cfg.Server, " ", err)
			time.Sleep(time.Second * 5)
			continue
		}
		if err = websocket.Message.Send(conn, cfg.Secret); err != nil {
			time.Sleep(time.Second * 5)
			continue
		}
		var rMessage map[string]interface{}
		if err := websocket.JSON.Receive(conn, &rMessage); err == nil {
			if rMessage["code"].(float64) != 0 {
				log.Error("connect to console server ", cfg.Server, " ", rMessage["msg"])
				return
			} else {
				log.Info("connect to console server ", cfg.Server, " success")
			}
		}
		for {
			var msg string
			err := websocket.Message.Receive(conn, &msg)
			if err != nil {
				log.Error("read console server error:", err)
				break
			} else {
				b, a, f := strings.Cut(msg, "\n")
				if f {
					if len(a) > 0 {
						req, err := http.NewRequest("POST", b, strings.NewReader(a))
						if err != nil {
							log.Error("read console server error:", err)
							break
						}
						h, _ := cfg.mux.Handler(req)
						h.ServeHTTP(wr, req)
					} else {
						req, err := http.NewRequest("GET", b, nil)
						if err != nil {
							log.Error("read console server error:", err)
							break
						}
						h, _ := cfg.mux.Handler(req)
						h.ServeHTTP(wr, req)
					}
				} else {

				}
			}
		}
	}
}

type socketIOWriter struct {
	Url    string // 定义哪个接口上报的数据;
	Secret string //
	myResponseWriter
	*gosocketio.Channel
}

type IOData struct {
	Type   string `json:"type"`
	Data   string `json:"data"`
	Secret string `json:"secret"`
}

func (w *socketIOWriter) Write(b []byte) (int, error) {
	data := IOData{
		Secret: w.Secret,
		Type:   w.Url,
		Data:   string(b),
	}
	result, err := json.Marshal(data)
	defer w.Close()
	if err == nil {
		return len(result), w.Emit("message", string(result))
	} else {
		return -1, nil
	}
}

func (w *socketIOWriter) WriteHeartbeat(secret string) error {
	return w.Emit("heartbeat", secret)
}

var ticker = time.NewTicker(time.Second * 3)

const (
	webSocketProtocol  = "ws://"
	socketioUrl        = "/socket.io/?EIO=3&transport=websocket&secret="
	msgDelayTime       = 4
	reConnectDelayTime = 10
)

// SocketIORemote SocketIO的双向通信；
func (cfg *Engine) SocketIORemote() {
	for {
		server := strings.Split(strings.TrimPrefix(cfg.Server, "socket.io://"), ":")
		port := "48088"
		if len(server) == 2 {
			port = server[1]
		} else {
			panic("url config error")
		}
		url := webSocketProtocol + server[0] + ":" + port + socketioUrl + cfg.Secret
		c, err := gosocketio.Dial(
			url,
			transport.GetDefaultWebsocketTransport(),
		)
		if err != nil {
			log.Error("connect to console server ", cfg.Server, " ", err)
			// 链接失败，5s后重试；
			time.Sleep(time.Second * reConnectDelayTime)
			continue
		} else {
			c.On(gosocketio.OnConnection, func(h *gosocketio.Channel) {
				log.Info("connect to console server ", cfg.Server, " success")
				wr := &socketIOWriter{Channel: h}
				// 监听并处理消息请求；
				go MessageHandler(c, cfg)
				ticker.Reset(time.Second * msgDelayTime)
				// 启动协程与服务端维持心跳；
				go func() {
					for range ticker.C {
						// 向服务端发送心跳请求
						wr.WriteHeartbeat(cfg.Secret)
					}
				}()
			})
			break
		}
	}
}

// SocketMsg 消息体
type SocketMsg struct {
	Ack  int                    `json:"ack"`
	Data map[string]interface{} `json:"data"`
}

// MessageHandler 消息处理函数
func MessageHandler(c *gosocketio.Client, cfg *Engine) {
	c.On("res", func(h *gosocketio.Channel, msg string) {
		socketMsg := &SocketMsg{}
		if err := json.Unmarshal([]byte(msg), socketMsg); err == nil {
			if _, found := socketMsg.Data["url"]; found {
				url := socketMsg.Data["url"].(string)
				if url != "" {
					req, _ := http.NewRequest("GET", socketMsg.Data["url"].(string), nil)
					http, _ := cfg.mux.Handler(req)
					writer := &socketIOWriter{Channel: h, Url: url, Secret: cfg.Secret}
					http.ServeHTTP(writer, req)
				}
			}
			log.Infof("msg:%+v ", socketMsg)
		}
	})
	c.On(gosocketio.OnDisconnection, func(h *gosocketio.Channel) {
		log.Error("server disconnection ", cfg.Server, "")
		ticker.Stop()
		time.AfterFunc(time.Second*reConnectDelayTime, func() {
			go cfg.SocketIORemote()
		})
	})

	c.On(gosocketio.OnError, func(h *gosocketio.Channel) {
		log.Error("server OnError ", cfg.Server, "")
		ticker.Stop()
		time.AfterFunc(time.Second*reConnectDelayTime, func() {
			go cfg.SocketIORemote()
		})
	})
}

func (cfg *Engine) OnEvent(event any) {
	switch v := event.(type) {
	case context.Context:
		util.RTPReorderBufferLen = uint16(cfg.RTPReorderBufferLen)
		if strings.HasPrefix(cfg.Console.Server, "wss") {
			go cfg.WsRemote()
		} else if strings.HasPrefix(cfg.Console.Server, "socket.io") {
			go cfg.SocketIORemote()
		} else {
			go cfg.Remote(v)
		}
	}
}

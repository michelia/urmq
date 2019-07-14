# urmq(util of RabbitMQ)

使用 [github.com/streadway/amqp]()操作MQ消息, 包括了重连及重发.

使用[github.com/rs/zerolog]()操作日志, 每个日志都带有`GUID`, 方便分析异常.

# 样例
```go
package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/michelia/urmq"
	zlog "github.com/rs/zerolog/log"
)

func main() {
	// 带session的log
	slog := zlog.With().
		Str("service", "xxx").
		Logger()

	// 构建rmq
	rmq := urmq.NewRMQ(&slog,
		"amqp://guest:guest@127.0.0.1:5672", // url:              rmq 连接地址.
		"getExchange",                       // getExchange:      从这个Exchange获取上游过来的消息 .
		"getQueue",                          // getQueue:         接收上游消息的队列, .
		nil,                                 // bindKeys:         绑定到接收消息队列 的key 即主题 .
		"sendExchange",                      // sendExchange:     默认向这个exchange推消息.
		"fanout",                            // sendExchangeKind: exchange类型.
	)

	// 优雅关闭
	sc := make(chan os.Signal, 1)
	signal.Notify(sc,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)
	go func() {
		sig := <-sc
		rmq.Close()
		slog.Fatal().Interface("Signal", sig).Msg("主动关闭退出")
	}()

	// 向默认的sendExchange发送消息
	rmq.Publish(&slog, // 带会话的上下问log
		[]byte("消息body"), // body:           数据体
		"routingKey",     // key:             主题, 可为空
		"消息id",           // correlationId:  可以理解为消息的guid, 可为空
		"",               // exchange:        交换机, 可为空 则使用默认的sendExchange
	)

	// 接受"getQueue"过来的消息
	for d := range rmq.MsgChan {
		// 操作接受到的消息d
		_ = d
		// ...
	}
	rmq.Done <- nil // 告诉rmq俺处理完了, 可以优雅关闭退出啦

	select{}
}

```


### 参考
[https://github.com/streadway/amqp/issues/133#issuecomment-102842493]()
[https://github.com/streadway/amqp/blob/master/_examples/simple-consumer/consumer.go]()

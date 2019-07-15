# urmq(util of RabbitMQ)

使用 [github.com/streadway/amqp]()操作MQ消息, 包括了重连及重发.

使用[github.com/rs/zerolog]()操作日志, 每个日志都带有`GUID`, 方便分析异常.

封装成回调风格.

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
	c := Config{
		Url:              "amqp://guest:guest@127.0.0.1:5672",
		Exchange:         "test",
		Queue:            "test",
		BindKeys:         []string{"test.#"},
		SendExchange:     "test_send",
		SendExchangeKind: "fanout",
	}
	// 带session的log
	slog := zlog.With().
		Str("service", "rmq_test").
		Logger()
	rmq := NewRMQ(&slog, c)
	go func() {
		rmq.Connect() // 连接
		rmq.Handle(func(msgChan <-chan amqp.Delivery) {
			slog.Print("consumer")
			for d := range msgChan {
				// 操作接受到的消息d
				_ = d
				// ...
			}
			slog.Print("consumer all")
			rmq.Done <- nil
		})
		slog.Print("consumer")
	}()
	rmq.Publish(&slog, []byte(`{"msg":"debug"}`), "", "")
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
		slog.Print("准备关闭连接")
		rmq.Close()
		slog.Fatal().Interface("Signal", sig).Msg("主动关闭退出")
	}()

	select{}
}

```

### 参考
- [https://github.com/streadway/amqp/issues/133#issuecomment-102842493]()
- [https://github.com/streadway/amqp/blob/master/_examples/simple-consumer/consumer.go]()
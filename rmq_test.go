package urmq

import (
	"testing"
	"time"

	zlog "github.com/rs/zerolog/log"
	"github.com/streadway/amqp"
)

func TestNew(t *testing.T) {
	c := Config{
		Url:      "amqp://guest:guest@127.0.0.1:5672",
		Exchange: "test",
		Queue:    "test",
		BindKeys: []string{"test.#"},
	}
	// 带session的log
	slog := zlog.With().
		Str("service", "rmq_test").
		Logger()
	rmq := NewRMQ(&slog, c)
	rmq.connect() // 连接

	// 接受"getQueue"过来的消息
	go rmq.Handle(func(msgChan <-chan amqp.Delivery) {
		slog.Print("consumer")
		for d := range msgChan {
			// 操作接受到的消息d
			_ = d
			// ...
		}
		slog.Print("consumer all")
		rmq.Done <- struct{}{}
	})
	slog.Print("consumer")
	time.Sleep(1e9 * 50)
	slog.Print("准备关闭连接")
	rmq.Close()
	time.Sleep(1e9 * 3)
}

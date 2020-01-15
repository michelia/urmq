package urmq

// https://github.com/streadway/amqp/issues/133#issuecomment-102842493

import (
	"errors"
	"time"

	"github.com/michelia/ulog"
	"github.com/streadway/amqp"
)

type Delivery = amqp.Delivery

type RMQ struct {
	slog        ulog.Logger
	conn        *amqp.Connection
	channel     *amqp.Channel
	ctag        string               // ConsumerTag 在r.channel.Cancel中使用的, 这里取GetQueue的值
	msgChan     <-chan amqp.Delivery // 获取getQueue中的消息
	isReconnect chan error           // 通知consumer连接关闭, 需重新构建consumer
	config      Config
}

// Config 构建rmq需要的配置
type Config struct {
	Url              string   // rmq 连接地址.
	Exchange         string   // 从这个Exchange获取上游过来的消息 .
	Queue            string   // Consumer 接收上游消息的队列, .
	BindKeys         []string // 绑定到接收消息队列 的key 即主题 .
	Threads          int      // Consumer handel并行数量
	SendExchange     string   // 默认向这个exchange推消息.
	SendExchangeKind string   // exchange类型.
}

/*
NewRMQ 创建操作RMQ消息的对象

slog: 带session的log
config: 构建rmq需要的配置
*/
func NewRMQ(slog ulog.Logger, config Config) *RMQ {
	sl := slog.With().Str("action", "rmq").Logger()
	if config.Threads == 0 {
		config.Threads = 1
	}
	r := RMQ{
		slog:        &sl,
		isReconnect: make(chan error),
		config:      config,
	}
	return &r
}

func (r *RMQ) Connect() {
	var err error
	r.conn, err = amqp.Dial(r.config.Url)
	if err != nil {
		r.slog.Error().Caller().Err(err).Msg("amqp.Dial error")
		time.Sleep(5 * time.Second)
		r.Connect()
		return
	}
	r.channel, err = r.conn.Channel()
	if err != nil {
		r.slog.Error().Caller().Err(err).Msg("r.conn.Channel error")
		time.Sleep(5 * time.Second)
		r.Connect()
		return
	}
	go r.reConnect() // 启动重连协程 (注意每次连接后, 都要执行)
	r.slog.Print("rmq connect success")
	r.init() // 每次连接后需要执行的操作 声明 队列 交换机 执行绑定
}

// reconnect 连接异常关闭的时候重连
func (r *RMQ) reConnect() {
	// 注册一个监听关闭连接的事件
	err := <-r.conn.NotifyClose(make(chan *amqp.Error))
	if err != nil {
		// 如果不为nil, 则属于异常异常事件
		r.slog.Error().Err(err).Caller().Msg("rmq conn error, wait for 2s and reconnet")
		time.Sleep(1e9 * 2)
		r.channel.Close()
		r.Connect()
		r.slog.Print("rmq reconnect success")
		r.isReconnect <- errors.New("reconnect")
		return
	}
	// On normal shutdowns, the chan will be closed.
	// 执行到这里是用户正常关闭, 就不要重连啦
	r.slog.Print("rmq conn shutdowns")
	return
}

// init 每次连接后需要执行的操作 声明 队列 交换机 执行绑定
func (r *RMQ) init() {
nextReconnet:
	var err error
	// 创建接受消息的队列
	if r.config.Queue != "" {
		_, err = r.channel.QueueDeclare(
			r.config.Queue, // name
			true,           // durable
			false,          // delete when usused
			false,          // exclusive
			false,          // no-wait
			nil,            // arguments
		)
		if err != nil {
			r.slog.Error().Err(err).Caller().Msg("QueueDeclare error")
			time.Sleep(time.Second * 5)
			goto nextReconnet
		}
		r.ctag = r.config.Queue // consumer tag
		r.msgChan, err = r.channel.Consume(
			r.config.Queue, // queue
			r.ctag,         // ConsumerTag
			false,          // auto-ack
			false,          // exclusive
			false,          // no-local
			false,          // no-wait
			nil,            // args
		)
		if err != nil {
			r.slog.Fatal().Caller().Err(err).Msg("MqCh.Consume error")
			time.Sleep(time.Second * 5)
			goto nextReconnet
		}
		// 队列绑定key
		if r.config.Exchange != "" && len(r.config.BindKeys) > 0 {
			for _, key := range r.config.BindKeys {
				if err = r.channel.QueueBind(
					r.config.Queue,    // name of the queue
					key,               // bindingKey
					r.config.Exchange, // sourceExchange
					false,             // noWait
					nil,               // arguments
				); err != nil {
					r.slog.Error().Err(err).Caller().Msg("QueueBind error")
					time.Sleep(time.Second * 5)
					goto nextReconnet
				}
			}
		}
	}
	// 如果不为空则需要建推送消息的exchange
	if r.config.SendExchange != "" && r.config.SendExchangeKind != "" {
		err = r.channel.ExchangeDeclare(
			r.config.SendExchange,     // ExchangeName
			r.config.SendExchangeKind, // kind
			true,                      // durable
			false,                     // delete when usused
			false,                     // exclusive
			false,                     // no-wait
			nil,                       // arguments)
		)
		if err != nil {
			r.slog.Error().Err(err).Caller().Msg("ExchangeDeclare error")
			time.Sleep(time.Second * 5)
			goto nextReconnet
		}
	}
	r.slog.Print("After successful connection, rmq init success.")
}

// Close 优雅的关闭rmq, 这样不丢失消息
func (r *RMQ) Close() {
	if r.ctag != "" {
		if err := r.channel.Cancel(r.ctag, true); err != nil {
			r.slog.Error().Caller().Err(err).Msg("Consumer cancel failed")
		}
	}
	if err := r.channel.Close(); err != nil {
		r.slog.Error().Caller().Err(err).Msg("channel close failed")
	}
	if err := r.conn.Close(); err != nil {
		r.slog.Error().Caller().Err(err).Msg("AMQP connection close error")
	}
	r.slog.Print("closed rmq closed , wait for msg")
	r.isReconnect <- nil
	r.slog.Print("Successful close rmq")
}

func (r *RMQ) Handle(handler func(deliveries <-chan amqp.Delivery)) {
	for {
		for i := 0; i < r.config.Threads; i++ {
			go handler(r.msgChan)
		}
		r.slog.Print("已构建consumer")
		// Go into reconnect loop when
		// r.isReconnect is passed non nil values
		if err := <-r.isReconnect; err != nil {
			r.slog.Error().Err(err).Caller().Msg("重连需重新构建consumer")
			continue
		} else {
			r.slog.Print("consumer close connect")
			return
		}
	}
}

/*
Publish 向exchange中推送消息, 并带有重试机制

slog:             带session的log
body:             数据体  .
key:              主题, 可为空  .
correlationId:    可以理解为消息的guid, 可为空  .
*/
func (r *RMQ) Publish(slog ulog.Logger, body []byte, key, correlationId string) {
	// 重试 60次, 5分钟
	for i := 0; i < 60; i++ {
		err := r.channel.Publish(
			r.config.SendExchange, // exchange publish to an exchange
			key,                   // routingKey routing to 0 or more queues, fanout 忽略 routingKey
			false,                 // mandatory
			false,                 // immediate
			amqp.Publishing{
				ContentType:   "application/json",
				CorrelationId: correlationId,
				Body:          body,
				DeliveryMode:  amqp.Persistent, // 0/1=non-persistent, 2=persistent
				// Headers:       amqp.Table{},
				// Priority:      0,               // 0-9
			},
		)
		if err != nil {
			e, ok := err.(*amqp.Error)
			if !ok {
				slog.Error().Caller().Err(err).Msg("push-rmq-error, not amqp.Error")
				return
			}
			switch e {
			case amqp.ErrClosed:
				slog.Warn().Msg("push-rmq-error-reconnect")
				time.Sleep(time.Second * 5)
				continue
			default:
				slog.Error().Caller().Err(err).Msg("push-rmq-error, other error")
				return
			}
		}
		slog.Print("push-rmq-success")
		return
	}
}

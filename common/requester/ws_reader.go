package requester

import (
	"bytes"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

type wsReader[T streamable] struct {
	reader        *websocket.Conn
	handlerPrefix HandlerPrefix[T]

	DataChan chan T
	ErrChan  chan error

	closeOnce  sync.Once
	recvCalled atomic.Bool
}

func (stream *wsReader[T]) Recv() (<-chan T, <-chan error) {
	stream.recvCalled.Store(true)
	go stream.processLines()
	return stream.DataChan, stream.ErrChan
}

func (stream *wsReader[T]) processLines() {
	// ✅ 确保函数退出时关闭 channels，防止 goroutine 泄漏
	defer close(stream.DataChan)
	defer close(stream.ErrChan)

	for {
		_, msg, err := stream.reader.ReadMessage()
		if err != nil {
			stream.ErrChan <- err
			return
		}

		stream.handlerPrefix(&msg, stream.DataChan, stream.ErrChan)

		if msg == nil {
			continue
		}

		if bytes.Equal(msg, StreamClosed) {
			return
		}
	}
}

// Close 关闭底层 ws 连接并 drain pending channel send，避免 handler 在
// unbuffered channel 上的阻塞 send 导致 producer goroutine 泄漏。
// 实际的 drain + close 顺序逻辑见 DrainAndClose 的注释。
func (stream *wsReader[T]) Close() {
	stream.closeOnce.Do(func() {
		closer := func() { _ = stream.reader.Close() }

		if !stream.recvCalled.Load() {
			closer()
			return
		}

		DrainAndClose(stream.DataChan, stream.ErrChan, closer, "wsReader.Close")
	})
}

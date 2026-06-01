package requester

import (
	"bufio"
	"bytes"
	"done-hub/common/logger"
	"done-hub/types"
	"fmt"
	"net/http"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bytedance/gopkg/util/gopool"
)

var StreamClosed = []byte("stream_closed")

type HandlerPrefix[T streamable] func(rawLine *[]byte, dataChan chan T, errChan chan error)

type streamable interface {
	// types.ChatCompletionStreamResponse | types.CompletionResponse
	any
}

type StreamReaderInterface[T streamable] interface {
	Recv() (<-chan T, <-chan error)
	Close()
}

type streamReader[T streamable] struct {
	reader   *bufio.Reader
	response *http.Response
	NoTrim   bool

	handlerPrefix HandlerPrefix[T]

	DataChan chan T
	ErrChan  chan error

	closeOnce  sync.Once
	recvCalled atomic.Bool
}

func (stream *streamReader[T]) Recv() (<-chan T, <-chan error) {
	stream.recvCalled.Store(true)
	gopool.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				logger.SysError(fmt.Sprintf("Panic in streamReader.processLines: %v", r))
				logger.SysError(fmt.Sprintf("stacktrace from panic: %s", string(debug.Stack())))

				// processLines 的 defer 会先关闭 DataChan/ErrChan，panic 才冒泡到这里，
				// 此时 ErrChan 已关闭。直接 ErrChan <- err 会触发 "send on closed channel"
				// 二次 panic、整个进程崩溃。改成非阻塞 send + recover 兜底：能投就投，
				// 不能投就让 consumer 从已关 channel 读到 ok=false 自然退出。
				err := &types.OpenAIError{
					Code:    "system error",
					Message: "stream processing panic",
					Type:    "system_error",
				}
				defer func() { _ = recover() }()
				select {
				case stream.ErrChan <- err:
				default:
				}
			}
		}()
		stream.processLines()
	})

	return stream.DataChan, stream.ErrChan
}

//nolint:gocognit
func (stream *streamReader[T]) processLines() {
	// ✅ 确保函数退出时关闭 channels，防止 goroutine 泄漏
	defer close(stream.DataChan)
	defer close(stream.ErrChan)

	for {
		rawLine, readErr := stream.reader.ReadBytes('\n')

		// 先处理读取到的数据（即使有错误，ReadBytes 也可能返回部分数据）
		// 对于 NoTrim 流（relay 场景），跳过 EOF 时不以 '\n' 结尾的不完整数据，
		// 避免转发给客户端导致解析错误
		isIncomplete := stream.NoTrim && readErr != nil && len(rawLine) > 0 && rawLine[len(rawLine)-1] != '\n'

		if len(rawLine) > 0 && !isIncomplete {
			if !stream.NoTrim {
				rawLine = bytes.TrimSpace(rawLine)
			}

			if len(rawLine) > 0 {
				stream.handlerPrefix(&rawLine, stream.DataChan, stream.ErrChan)

				if rawLine != nil && bytes.Equal(rawLine, StreamClosed) {
					return
				}
			}
		}

		// 然后处理错误
		if readErr != nil {
			select {
			case stream.ErrChan <- readErr:
			case <-time.After(1000 * time.Millisecond):
				logger.SysError(fmt.Sprintf("无法发送流错误: %v", readErr))
			}
			return
		}
	}
}

// Close 既要关闭上游响应体，也要解除 producer goroutine 在 handler 内对
// DataChan/ErrChan 的阻塞 send，避免 HTTP/2 stream slot 泄漏。
// 实际的 drain + close 顺序逻辑见 DrainAndClose 的注释。
func (stream *streamReader[T]) Close() {
	stream.closeOnce.Do(func() {
		closer := func() {
			if stream.response != nil && stream.response.Body != nil {
				_ = stream.response.Body.Close()
			}
		}

		// Recv 从未调用过：没有 producer goroutine，也就没有阻塞 send 要解开，
		// 只关 body 即可。否则下面的 drain 会在永远不会关闭的 channel 上死等。
		if !stream.recvCalled.Load() {
			closer()
			return
		}

		DrainAndClose(stream.DataChan, stream.ErrChan, closer, "streamReader.Close")
	})
}

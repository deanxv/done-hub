package requester

import (
	"bufio"
	"bytes"
	"done-hub/common/logger"
	"done-hub/types"
	"fmt"
	"net/http"
	"runtime/debug"
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
}

func (stream *streamReader[T]) Recv() (<-chan T, <-chan error) {
	gopool.Go(func() {
		defer func() {

			if r := recover(); r != nil {
				logger.SysError(fmt.Sprintf("Panic in streamReader.processLines: %v", r))
				logger.SysError(fmt.Sprintf("stacktrace from panic: %s", string(debug.Stack())))

				err := &types.OpenAIError{
					Code:    "system error",
					Message: "stream processing panic",
					Type:    "system_error",
				}

				stream.ErrChan <- err
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

func (stream *streamReader[T]) Close() {
	stream.response.Body.Close()
}

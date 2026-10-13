package check_channel

import (
	"bytes"
	"done-hub/types"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/rand/v2"
	"strconv"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// 字体只解析一次；Face 不是并发安全的，每次渲染单独创建（开销很小）
var loadDigitFont = sync.OnceValues(func() (*opentype.Font, error) {
	return opentype.Parse(gobold.TTF)
})

// renderDigitsPNG 把数字渲染成白底黑字 PNG，以 data URL 返回。
// 图片内嵌在请求里，不依赖公网可访问的服务器地址；能读出数字才说明多模态内容真正到达了模型。
func renderDigitsPNG(digits string) (string, error) {
	f, err := loadDigitFont()
	if err != nil {
		return "", err
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: 96, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return "", err
	}

	const width, height = 480, 200
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)

	d := &font.Drawer{Dst: img, Src: image.NewUniform(color.Black), Face: face}
	textWidth := d.MeasureString(digits).Ceil()
	ascent := face.Metrics().Ascent.Ceil()
	d.Dot = fixed.P((width-textWidth)/2, (height+ascent)/2-8)
	d.DrawString(digits)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

type CheckImgProcess struct {
	ModelName string
	expected  string
	imageURL  string
	renderErr error
}

func CreateCheckImgProcess(modelName string) *CheckImgProcess {
	expected := strconv.Itoa(100000 + rand.IntN(900000))
	imageURL, err := renderDigitsPNG(expected)
	return &CheckImgProcess{ModelName: modelName, expected: expected, imageURL: imageURL, renderErr: err}
}

const imageProbeKey = "image"

func (c *CheckImgProcess) GetKey() string {
	return imageProbeKey
}

func (c *CheckImgProcess) GetRequest() *types.ChatCompletionRequest {
	return &types.ChatCompletionRequest{
		Model: c.ModelName,
		Messages: []types.ChatCompletionMessage{
			{
				Role: types.ChatMessageRoleUser,
				Content: []types.ChatMessagePart{
					{Type: types.ContentTypeText, Text: "Read the number in the image. Respond with ONLY the digits, nothing else."},
					{Type: types.ContentTypeImageURL, ImageURL: &types.ChatMessageImageURL{URL: c.imageURL}},
				},
			},
		},
	}
}

func (c *CheckImgProcess) Check(_ CheckContext, _ *types.ChatCompletionRequest, resp *types.ChatCompletionResponse, openaiErr *types.OpenAIError) []*CheckResult {
	if c.renderErr != nil {
		return []*CheckResult{{Name: "识图", Status: CheckStatusFailed, Remark: "生成检测图片失败：" + c.renderErr.Error()}}
	}
	if openaiErr != nil {
		return failedResponse(openaiErr)
	}

	result := &CheckResult{Name: "识图", Status: CheckStatusFailed}
	answer, ok := firstChoiceContent(resp)
	switch {
	case !ok:
		result.Remark = "未返回响应内容"
	case validateChallenge(answer, c.expected):
		result.Status = CheckStatusSuccess
		result.Remark = fmt.Sprintf("图中数字 %s，识别正确", c.expected)
	default:
		result.Remark = fmt.Sprintf("图中数字 %s，实际回答：%s（模型不支持识图，或中转丢弃了图片）", c.expected, truncate(answer, 200))
	}
	return []*CheckResult{result}
}

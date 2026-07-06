package tts

import (
	"GoAI/common/code"
	"GoAI/common/tts"
	"GoAI/controller"
	"bytes"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type (
	// TTSRequest 定义了文本转语音请求的数据结构
	// Text 是要转换的文本内容，必须提供
	TTSRequest struct {
		Text string `json:"text" binding:"required"`
	}

	// TTSResponse 定义了文本转语音响应的数据结构
	// TaskID 是异步任务的唯一标识符
	TTSResponse struct {
		TaskID string `json:"task_id,omitempty"`
		controller.Response
	}

	// QueryTTSResponse 定义了查询文本转语音任务状态响应的数据结构
	// TaskID 是任务的唯一标识符
	// TaskStatus 表示任务的当前状态
	// TaskResult 包含任务处理完成后的结果
	QueryTTSResponse struct {
		TaskID     string `json:"task_id,omitempty"`
		TaskStatus string `json:"task_status,omitempty"`
		TaskResult string `json:"task_result,omitempty"`
		controller.Response
	}

	// ASRResponse 定义语音识别响应。
	ASRResponse struct {
		Text string `json:"text,omitempty"`
		controller.Response
	}
)

const maxASRAudioSize = 10 * 1024 * 1024

// TTSController  封装了 TTS 服务相关功能，包含一个 TTS 服务实例
type TTSController struct {
	ttsService *tts.TTSService
}

// NewTTSController 创建并返回一个新的 TTSController 实例，初始化其中的 TTS 服务
func NewTTSController() *TTSController {
	return &TTSController{
		ttsService: tts.NewTTSService(),
	}
}

// CreateTTSTask 处理 TTS 任务创建请求
// 接收 JSON 格式的 TTS 请求参数，验证参数有效性后创建 TTS 任务
// 成功时返回包含任务 ID 的响应，失败时返回对应的错误码
// 任务结果需要前端通过任务 ID 轮询获取
func CreateTTSTask(c *gin.Context) {
	req := new(TTSRequest)
	res := new(TTSResponse)

	// 绑定请求参数到结构体
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}
	if req.Text == "" {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	// 创建 TTS 任务并返回任务 ID，由前端轮询查询结果
	ttsCtrl := NewTTSController()
	taskID, err := ttsCtrl.ttsService.CreateTTS(c, req.Text)
	if err != nil {
		c.JSON(http.StatusOK, res.CodeOf(code.TTSFail))
		return
	}

	res.Success()
	res.TaskID = taskID

	c.JSON(http.StatusOK, res)
}

// QueryTTSTask 查询指定TTS任务的状态
//
// 参数:
//   - c: gin上下文对象，包含请求参数
//
// 返回值:
//   - 通过gin上下文返回JSON响应，包含任务状态和结果
//   - 当task_id参数缺失时返回参数错误
//   - 当查询失败或任务不存在时返回失败状态
//   - 成功时返回任务ID、状态和语音URL(如果已生成)
func QueryTTSTask(c *gin.Context) {
	res := new(QueryTTSResponse)

	// 获取任务ID
	taskID := c.Query("task_id")
	if taskID == "" {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	// 查询 TTS 任务状态和结果
	ttsCtrl := NewTTSController()
	TTSQueryResponse, err := ttsCtrl.ttsService.QueryTTSFull(c, taskID)
	if err != nil {
		log.Println("语音合成失败", err.Error())
		c.JSON(http.StatusOK, res.CodeOf(code.TTSFail))
		return
	}

	// 检查 TasksInfo 是否为空，避免空指针异常
	if len(TTSQueryResponse.TasksInfo) == 0 {
		c.JSON(http.StatusOK, res.CodeOf(code.TTSFail))
		return
	}

	res.Success()
	res.TaskID = TTSQueryResponse.TasksInfo[0].TaskID
	// 检查 TaskResult 是否为 nil，避免空指针异常
	if TTSQueryResponse.TasksInfo[0].TaskResult != nil {
		res.TaskResult = TTSQueryResponse.TasksInfo[0].TaskResult.SpeechURL
	}
	res.TaskStatus = TTSQueryResponse.TasksInfo[0].TaskStatus

	c.JSON(http.StatusOK, res)
}

// RecognizeSpeech 处理语音输入转文字请求。
func RecognizeSpeech(c *gin.Context) {
	res := new(ASRResponse)

	file, err := c.FormFile("audio")
	if err != nil {
		log.Println("[RecognizeSpeech] FormFile fail ", err)
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}
	if file.Size <= 0 || file.Size > maxASRAudioSize {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}
	if !strings.HasSuffix(strings.ToLower(file.Filename), ".wav") {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	src, err := file.Open()
	if err != nil {
		log.Println("[RecognizeSpeech] file open fail ", err)
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}
	defer src.Close()

	audio, err := io.ReadAll(io.LimitReader(src, maxASRAudioSize+1))
	if err != nil {
		log.Println("[RecognizeSpeech] read audio fail ", err)
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}
	if len(audio) == 0 || len(audio) > maxASRAudioSize || !bytes.HasPrefix(audio, []byte("RIFF")) {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	ttsCtrl := NewTTSController()
	text, err := ttsCtrl.ttsService.RecognizeSpeech(c, audio)
	if err != nil {
		log.Println("[RecognizeSpeech] asr fail ", err)
		c.JSON(http.StatusOK, res.CodeOf(code.ASRFail))
		return
	}

	res.Success()
	res.Text = text
	c.JSON(http.StatusOK, res)
}

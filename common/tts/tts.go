package tts

import (
	"GoAI/config"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

// accessTokenResp 表示访问令牌的响应结构体
// AccessToken 字段包含获取到的访问令牌字符串
type accessTokenResp struct {
	AccessToken string `json:"access_token"`
}

// TTSRequest 定义了文本转语音请求的数据结构
type TTSRequest struct {
	Text           string `json:"text"`
	Format         string `json:"format"`
	Voice          int    `json:"voice"`
	Lang           string `json:"lang"`
	Speed          int    `json:"speed"`
	Pitch          int    `json:"pitch"`
	Volume         int    `json:"volume"`
	EnableSubtitle int    `json:"enable_subtitle"`
}

// TTSCreateResponse 表示创建TTS任务后的响应数据结构
// Go 的 json.Unmarshal 对字段不匹配的处理策略：
// JSON 中有多余字段 → 会被忽略，不会报错
// JSON 中缺少字段 → 该字段设为零值（string 为空字符串）
// JSON 字段类型不匹配 → 会返回解析错误
type TTSCreateResponse struct {
	TaskID string `json:"task_id"` // TaskID 是百度TTS API返回的任务唯一标识符
}

// TTSTaskResult 表示TTS任务的返回结果
// SpeechURL 字段包含生成的语音文件的URL
type TTSTaskResult struct {
	SpeechURL string `json:"speech_url,omitempty"`
}

// TTSTask 表示一个TTS(文本转语音)任务的结构体
// TaskID: 任务的唯一标识符
// TaskStatus: 任务的当前状态
// TaskResult: 任务完成后的结果，未完成时为nil
type TTSTask struct {
	TaskID     string         `json:"task_id"`
	TaskStatus string         `json:"task_status"`
	TaskResult *TTSTaskResult `json:"task_result,omitempty"`
}

// TTSQueryResponse 表示文本转语音查询的响应结构
// LogID: 请求的唯一日志标识符
// TasksInfo: 包含文本转语音任务信息的数组
type TTSQueryResponse struct {
	LogID     string    `json:"log_id"`
	TasksInfo []TTSTask `json:"tasks_info"`
}

// ASRRequest 定义百度短语音识别请求结构。
type ASRRequest struct {
	Format  string `json:"format"`
	Rate    int    `json:"rate"`
	Channel int    `json:"channel"`
	Token   string `json:"token"`
	Cuid    string `json:"cuid"`
	Len     int    `json:"len"`
	Speech  string `json:"speech"`
	DevPID  int    `json:"dev_pid"`
}

// ASRResponse 表示百度短语音识别响应结构。
type ASRResponse struct {
	ErrNo  int      `json:"err_no"`
	ErrMsg string   `json:"err_msg"`
	Result []string `json:"result"`
}

// TTSService 提供文本转语音(TTS)相关服务功能
type TTSService struct{}

// NewTTSService 创建并返回一个新的TTSService实例
func NewTTSService() *TTSService {
	return &TTSService{}
}

// GetAccessToken 从百度语音服务获取访问令牌
//
// 使用配置中的 TTS 客户端 ID 和密钥进行认证
//
// 返回获取到的访问令牌字符串，如果获取失败则返回空字符串
func (s *TTSService) GetAccessToken() string {
	conf := config.GetConfig()

	url := "https://aip.baidubce.com/oauth/2.0/token"
	postData := fmt.Sprintf(
		"grant_type=client_credentials&client_id=%s&client_secret=%s",
		conf.TTSConfig.TTSClientId,
		conf.TTSConfig.TTSClientSecret,
	)

	// 发送 POST 请求
	resp, err := http.Post(url, "application/x-www-form-urlencoded", bytes.NewReader([]byte(postData)))
	if err != nil {
		log.Println("get token error:", err)
		return ""
	}
	defer resp.Body.Close()

	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Println("read token error:", err)
		return ""
	}

	// 解析响应体
	var tokenResp accessTokenResp
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		log.Println("unmarshal token error:", err)
		return ""
	}

	return tokenResp.AccessToken
}

// CreateTTS 使用百度语音合成API创建TTS任务
//
// 参数:
//   - ctx: 上下文对象，用于控制请求的生命周期
//   - text: 需要合成的文本内容
//
// 返回值:
//   - string: 成功时返回任务ID
//   - error: 失败时返回错误信息
func (s *TTSService) CreateTTS(ctx context.Context, text string) (string, error) {
	// 获取 access token
	accessToken := s.GetAccessToken()
	if accessToken == "" {
		return "", fmt.Errorf("failed to get access token")
	}

	// 构造请求体
	payload := TTSRequest{
		Text:           text,
		Format:         "mp3-16k",
		Voice:          4194,
		Lang:           "zh",
		Speed:          5,
		Pitch:          5,
		Volume:         5,
		EnableSubtitle: 0,
	}

	// 序列化请求体为 json
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	// 构造 http 请求
	url := "https://aip.baidubce.com/rpc/2.0/tts/v1/create?access_token=" + accessToken
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// 发送请求
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// 读取响应体
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	log.Println("[TTS Create] raw:", string(respBody))

	// 解析响应体
	var result TTSCreateResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}

	// 检查任务ID是否为空
	if result.TaskID == "" {
		return "", fmt.Errorf("create tts failed: empty task_id")
	}

	return result.TaskID, nil
}

// QueryTTSFull 查询指定 TTS 任务的完整状态信息
// 参数:
//
//	ctx: 上下文对象，用于控制请求生命周期
//	taskID: 要查询的TTS任务ID
//
// 返回值:
//
//	*TTSQueryResponse: 包含任务查询结果的响应结构体
//	error: 查询过程中发生的错误，成功时为nil
//
// 注意:
//
//	需要有效的access token才能执行查询
//	当任务状态为"Success"时会解析任务结果
func (s *TTSService) QueryTTSFull(ctx context.Context, taskID string) (*TTSQueryResponse, error) {
	// 获取 access token
	accessToken := s.GetAccessToken()
	if accessToken == "" {
		return nil, fmt.Errorf("failed to get access token")
	}

	// 构造请求体
	reqBody := map[string][]string{
		"task_ids": {taskID},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	// 构造 http 请求
	url := "https://aip.baidubce.com/rpc/2.0/tts/v1/query?access_token=" + accessToken
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// 发送请求
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 读取响应体
	respBody, _ := io.ReadAll(resp.Body)

	log.Println("[TTS Query] raw:", string(respBody))

	// 第三方 api 返回原始 JSON
	var rawResp struct {
		LogID     json.Number `json:"log_id"`
		TasksInfo []struct {
			TaskID     string          `json:"task_id"`
			TaskStatus string          `json:"task_status"`
			TaskResult json.RawMessage `json:"task_result,omitempty"` // 不会立即解析 json.RawMessage 字段，而是保存原始 JSON 字节
		} `json:"tasks_info"`
	}

	// 解析响应体
	if err := json.Unmarshal(respBody, &rawResp); err != nil {
		return nil, err
	}

	// 构造返回值，包含 TasksInfo 数组
	result := &TTSQueryResponse{
		LogID:     rawResp.LogID.String(),
		TasksInfo: make([]TTSTask, 0, len(rawResp.TasksInfo)),
	}

	// 遍历并转换任务信息，将 TTS API 的原始响应 (rawResp) 转换为更易于使用的自定义格式 (result)
	for _, t := range rawResp.TasksInfo {
		// 遍历原始响应中的所有任务
		task := TTSTask{
			TaskID:     t.TaskID,
			TaskStatus: t.TaskStatus,
			TaskResult: nil, // 默认 nil
		}

		// 如果任务状态为"Success"且有任务结果，则解析任务结果
		if t.TaskStatus == "Success" && len(t.TaskResult) > 0 {
			var r TTSTaskResult
			if err := json.Unmarshal(t.TaskResult, &r); err != nil {
				log.Println("[TTS Query] parse task_result error:", err)
				return nil, fmt.Errorf("failed to parse task result: %v", err)
			}
			task.TaskResult = &r
		}

		// 添加任务到结果数组
		result.TasksInfo = append(result.TasksInfo, task)
	}

	return result, nil
}

// RecognizeSpeech 使用百度短语音识别 API 将 16kHz 单声道 WAV 转成文本。
func (s *TTSService) RecognizeSpeech(ctx context.Context, audio []byte) (string, error) {
	if len(audio) == 0 {
		return "", fmt.Errorf("empty audio")
	}

	accessToken := s.GetAccessToken()
	if accessToken == "" {
		return "", fmt.Errorf("failed to get access token")
	}

	payload := ASRRequest{
		Format:  "wav",
		Rate:    16000,
		Channel: 1,
		Token:   accessToken,
		Cuid:    "gopherai",
		Len:     len(audio),
		Speech:  base64.StdEncoding.EncodeToString(audio),
		DevPID:  1537,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://vop.baidu.com/server_api", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	log.Println("[ASR] raw:", string(respBody))

	var result ASRResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}
	if result.ErrNo != 0 {
		return "", fmt.Errorf("asr failed: %s", result.ErrMsg)
	}
	if len(result.Result) == 0 || result.Result[0] == "" {
		return "", fmt.Errorf("empty asr result")
	}

	return result.Result[0], nil
}

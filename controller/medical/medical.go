package medical

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"GoAI/common/aihelper"
	core "GoAI/common/medical"
	"GoAI/common/mysql"
	"GoAI/common/rag"
	"GoAI/model"
	medicalservice "GoAI/service/medical"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var parsingSlots = make(chan struct{}, 2)

func fail(c *gin.Context, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, medicalservice.ErrNotFound) {
		status = http.StatusNotFound
	}
	if errors.Is(err, medicalservice.ErrBusy) {
		status = http.StatusConflict
	}
	c.JSON(status, gin.H{"status_code": 3001, "status_msg": err.Error()})
}

func CreateReport(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 11<<20)
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		fail(c, fmt.Errorf("请提交不超过10MB的报告文件或文字"))
		return
	}
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	text := strings.TrimSpace(c.PostForm("text"))
	name := "文字报告"
	path := ""
	if text != "" && len(text) > 60000 {
		fail(c, fmt.Errorf("报告文字最多60000字节"))
		return
	}
	if text == "" {
		file, err := c.FormFile("file")
		if err != nil {
			fail(c, fmt.Errorf("请选择PDF、JPG、PNG或TXT报告，或粘贴文字"))
			return
		}
		ext := strings.ToLower(filepath.Ext(file.Filename))
		if ext != ".pdf" && ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".txt" {
			fail(c, fmt.Errorf("只支持PDF、JPG、PNG、TXT"))
			return
		}
		if file.Size <= 0 || file.Size > 10<<20 {
			fail(c, fmt.Errorf("文件须在10MB以内"))
			return
		}
		src, err := file.Open()
		if err != nil {
			fail(c, err)
			return
		}
		defer src.Close()
		dst, err := os.CreateTemp("", "medical-report-*"+ext)
		if err != nil {
			fail(c, err)
			return
		}
		path = dst.Name()
		_, err = io.Copy(dst, io.LimitReader(src, (10<<20)+1))
		closeErr := dst.Close()
		if err != nil || closeErr != nil {
			os.Remove(path)
			fail(c, fmt.Errorf("保存报告失败"))
			return
		}
		name = filepath.Base(file.Filename)
		if len([]rune(name)) > 200 {
			name = string([]rune(name)[:200])
		}
	}
	select {
	case parsingSlots <- struct{}{}:
	default:
		if path != "" {
			os.Remove(path)
		}
		c.JSON(http.StatusTooManyRequests, gin.H{"status_msg": "报告解析繁忙，请稍后重试"})
		return
	}
	report := &model.MedicalReport{ID: uuid.NewString(), UserName: c.GetString("userName"), Name: name, Status: "parsing", Text: text, Indicators: "[]"}
	if err := mysql.DB.WithContext(c.Request.Context()).Create(report).Error; err != nil {
		<-parsingSlots
		if path != "" {
			os.Remove(path)
		}
		fail(c, err)
		return
	}
	go parseReport(report.ID, path, text)
	c.JSON(http.StatusAccepted, gin.H{"status_code": 1000, "report": report})
}

func parseReport(id, path, text string) {
	defer func() {
		<-parsingSlots
		if path != "" {
			os.Remove(path)
		}
		if recover() != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			mysql.DB.WithContext(ctx).Model(&model.MedicalReport{}).Where("id = ? AND status = ?", id, "parsing").
				Updates(map[string]any{"status": "failed", "error": "解析任务中断，请重新上传或手工录入指标"})
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	var err error
	if path != "" {
		text, err = rag.ExtractMedicalDocument(ctx, path)
	}
	if err == nil && (len(text) > 60000 || strings.TrimSpace(text) == "") {
		err = fmt.Errorf("报告文字为空或超过60000字节，请缩小报告范围")
	}
	indicators := "[]"
	status := "ready"
	errorText := ""
	if err == nil {
		indicators, err = aihelper.ExtractMedicalIndicators(ctx, text)
		if err == nil {
			var items []core.Indicator
			err = json.Unmarshal([]byte(indicators), &items)
			if err == nil {
				err = core.ValidateIndicators(items)
			}
			if err == nil {
				indicators = core.JSON(items)
			}
		}
	}
	if err != nil {
		status = "failed"
		indicators = "[]"
		errorText = "解析失败，请核对原文后手工录入指标，或重新上传。"
	}
	finish, cancelFinish := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFinish()
	mysql.DB.WithContext(finish).Model(&model.MedicalReport{}).Where("id = ? AND status = ?", id, "parsing").Updates(map[string]any{"text": text, "indicators": indicators, "status": status, "error": errorText})
}

func GetReport(c *gin.Context) {
	r, err := medicalservice.GetReport(c.Request.Context(), c.GetString("userName"), c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status_code": 1000, "report": r})
}
func ConfirmReport(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 300000)
	var req struct {
		Indicators []core.Indicator `json:"indicators"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, err)
		return
	}
	r, err := medicalservice.ConfirmReport(c.Request.Context(), c.GetString("userName"), c.Param("id"), req.Indicators)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status_code": 1000, "report": r})
}
func Runs(c *gin.Context) {
	runs, err := medicalservice.Runs(c.Request.Context(), c.GetString("userName"), c.Query("sessionId"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status_code": 1000, "runs": runs})
}

// Chat is called from the existing chat routes only for medical_agent.
func Chat(c *gin.Context, question, session, report string, stream bool) {
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	started := false
	var emit func(core.Event)
	if stream {
		emit = func(e core.Event) {
			if !started {
				c.Header("Content-Type", "text/event-stream")
				c.Header("Cache-Control", "no-cache")
				c.Header("X-Accel-Buffering", "no")
				started = true
			}
			if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", core.JSON(e)); err != nil {
				cancel()
				return
			}
			c.Writer.Flush()
		}
	}
	run, err := medicalservice.Run(ctx, c.GetString("userName"), medicalservice.ChatRequest{Question: question, SessionID: session, ReportID: report}, emit)
	if err != nil {
		if started {
			emit(core.Event{Type: "error", Summary: err.Error(), Status: "failed"})
		} else {
			fail(c, err)
		}
		return
	}
	if stream {
		fmt.Fprint(c.Writer, "data: [DONE]\n\n")
		c.Writer.Flush()
		return
	}
	c.JSON(http.StatusOK, gin.H{"status_code": 1000, "sessionId": run.SessionID, "Information": run.Answer, "run": run})
}

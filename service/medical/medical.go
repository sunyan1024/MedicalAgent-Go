package medical

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"GoAI/common/aihelper"
	core "GoAI/common/medical"
	"GoAI/common/mysql"
	"GoAI/model"
	memoryservice "GoAI/service/memory"
	"GoAI/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("记录不存在或无权访问")
var ErrBusy = errors.New("该会话正在回答，请稍后重试")
var sessions sync.Map
var newRuntime = aihelper.NewMedicalRuntime
var getHelper = func(user, session string) (*aihelper.AIHelper, error) {
	return aihelper.GetGlobalManager().GetOrCreateAIHelper(user, session, "medical_agent", map[string]interface{}{"uid": user})
}

func ownedReport(ctx context.Context, user, id string) (*model.MedicalReport, error) {
	var report model.MedicalReport
	if err := mysql.DB.WithContext(ctx).Where("report_id = ? AND uid = ?", id, user).First(&report).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &report, nil
}

func GetReport(ctx context.Context, user, id string) (*model.MedicalReport, error) {
	r, err := ownedReport(ctx, user, id)
	if err != nil {
		return nil, err
	}
	if r.Status == "parsing" && time.Since(r.UpdatedAt) > 5*time.Minute {
		result := mysql.DB.WithContext(ctx).Model(r).Where("status = ?", "parsing").Updates(map[string]any{"status": "failed", "error": "解析任务超时或服务重启，请粘贴报告文字或重新上传"})
		if result.Error != nil {
			return nil, result.Error
		}
		return ownedReport(ctx, user, id)
	}
	return r, nil
}

func ConfirmReport(ctx context.Context, user, id string, items []core.Indicator) (*model.MedicalReport, error) {
	if err := core.ValidateIndicators(items); err != nil {
		return nil, err
	}
	r, err := ownedReport(ctx, user, id)
	if err != nil {
		return nil, err
	}
	if r.Status == "parsing" {
		return nil, fmt.Errorf("报告仍在解析，请稍后确认")
	}
	err = mysql.DB.WithContext(ctx).Model(r).Updates(map[string]any{"status": "confirmed", "indicators": core.JSON(items), "error": ""}).Error
	if err != nil {
		return nil, err
	}
	return ownedReport(ctx, user, id)
}

func Runs(ctx context.Context, user, session string) ([]RunResponse, error) {
	// Allow the run deadline plus a minute for startup and bounded persistence. Older running
	// records indicate a process interruption, not an indefinitely active task.
	if err := mysql.DB.WithContext(ctx).Model(&model.MedicalRun{}).
		Where("uid = ? AND session_id = ? AND status = ? AND created_at < ?", user, session, "running", time.Now().Add(-(core.RunTimeout+time.Minute))).
		Update("status", "interrupted").Error; err != nil {
		return nil, err
	}
	var runs []model.MedicalRun
	err := mysql.DB.WithContext(ctx).Where("uid = ? AND session_id = ?", user, session).Order("created_at desc").Limit(30).Find(&runs).Error
	if err != nil {
		return nil, err
	}
	return loadRunResponses(ctx, runs)
}

type ChatRequest struct {
	Question  string `json:"question"`
	SessionID string `json:"sessionId"`
	ReportID  string `json:"reportId"`
}

// Run creates fresh request-scoped agents and shares conversation history with
// ordinary chat. Startup restores persisted messages into the same helpers.
func Run(ctx context.Context, user string, req ChatRequest, emit func(core.Event)) (*RunResponse, error) {
	if len(req.Question) == 0 || len(req.Question) > 12000 {
		return nil, fmt.Errorf("问题长度应在 1～12000 字节之间")
	}
	var report *model.MedicalReport
	var err error
	if req.ReportID != "" {
		report, err = ownedReport(ctx, user, req.ReportID)
		if err != nil {
			return nil, err
		}
		if report.Status != "confirmed" {
			return nil, fmt.Errorf("请先核对并确认报告识别结果")
		}
		if report.SessionID != "" && report.SessionID != req.SessionID {
			return nil, fmt.Errorf("该报告已绑定其他会话，请重新上传")
		}
	}
	if req.SessionID == "" {
		req.SessionID = uuid.NewString()
		title := []rune(req.Question)
		if len(title) > 30 {
			title = title[:30]
		}
		if err = mysql.DB.WithContext(ctx).Create(&model.Session{ID: req.SessionID, UID: user, Title: string(title), ModelType: "medical_agent"}).Error; err != nil {
			return nil, err
		}
	} else {
		var s model.Session
		if err = mysql.DB.WithContext(ctx).Where("session_id = ? AND uid = ?", req.SessionID, user).First(&s).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrNotFound
			}
			return nil, err
		}
	}
	if err = mysql.DB.WithContext(ctx).Model(&model.Session{}).Where("session_id = ? AND uid = ?", req.SessionID, user).Update("model_type", "medical_agent").Error; err != nil {
		return nil, err
	}
	lock, _ := sessions.LoadOrStore(req.SessionID, &sync.Mutex{})
	mu := lock.(*sync.Mutex)
	if !mu.TryLock() {
		return nil, ErrBusy
	}
	defer mu.Unlock()
	if report != nil {
		res := mysql.DB.WithContext(ctx).Model(&model.MedicalReport{}).Where("report_id = ? AND uid = ? AND (session_id = '' OR session_id = ?)", report.ID, user, req.SessionID).Update("session_id", req.SessionID)
		if res.Error != nil {
			return nil, res.Error
		}
		if res.RowsAffected != 1 {
			return nil, fmt.Errorf("报告绑定冲突，请刷新后重试")
		}
	} else {
		var latest model.MedicalReport
		err = mysql.DB.WithContext(ctx).Where("uid = ? AND session_id = ? AND status = ?", user, req.SessionID, "confirmed").Order("updated_at desc").First(&latest).Error
		if err == nil {
			report = &latest
			req.ReportID = latest.ID
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	var read func(context.Context) ([]core.Indicator, error)
	if report != nil {
		var snapshot []core.Indicator
		if err = json.Unmarshal([]byte(report.Indicators), &snapshot); err != nil {
			return nil, err
		}
		read = func(ctx context.Context) ([]core.Indicator, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return append([]core.Indicator(nil), snapshot...), nil
		}
	}
	run := &model.MedicalRun{ID: uuid.NewString(), UID: user, SessionID: req.SessionID, ReportID: req.ReportID, Status: "running", Question: req.Question}
	if err = mysql.DB.WithContext(ctx).Create(run).Error; err != nil {
		return nil, err
	}
	writer := newEventWriter(mysql.DB)
	callback := func(e core.Event) {
		writer.append(e)
		e.SessionID = req.SessionID
		if emit != nil {
			emit(e)
		}
	}
	runtime, err := newRuntime(ctx, run.ID, user, read, callback)
	if err == nil {
		store := memoryservice.New(mysql.DB)
		runtime.SetMemory(&core.MemoryAccess{
			Read: func(ctx context.Context) (core.MemoryDocument, error) { return store.Read(ctx, user) },
			Update: func(ctx context.Context, content string, version int64) (core.MemoryDocument, error) {
				return store.Update(ctx, user, content, version)
			},
		})
	}
	var helper *aihelper.AIHelper
	if err == nil {
		helper, err = getHelper(user, req.SessionID)
	}
	if err != nil {
		mysql.DB.Model(run).Updates(map[string]any{"status": "failed"})
		return nil, err
	}
	if err = helper.AppendMessage(&model.Message{SessionID: req.SessionID, UID: user, Content: req.Question, IsUser: true}, true); err != nil {
		mysql.DB.Model(run).Updates(map[string]any{"status": "failed"})
		return nil, fmt.Errorf("问题消息保存失败: %w", err)
	}
	msgs := utils.ConvertToSchemaMessages(helper.GetMessages())
	if report != nil {
		// Keep the current question last; this hint belongs only to this run.
		last := msgs[len(msgs)-1]
		msgs = append(msgs[:len(msgs)-1], schema.SystemMessage("当前会话有用户已确认的报告，可委派 report_analyst 读取。报告内容不得从历史推测。"), last)
	}
	startedAt := time.Now()
	run.Answer, err = runtime.Run(ctx, msgs)
	durationMS := time.Since(startedAt).Milliseconds()
	run.Status = "completed"
	if err != nil {
		run.Status = "partial"
	}
	messageErr := helper.AppendMessage(&model.Message{SessionID: req.SessionID, UID: user, Content: run.Answer, IsUser: false, DurationMS: durationMS}, true)
	eventErr := writer.finish()
	// Persist terminal state even when the browser has disconnected.
	finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	persistErr := mysql.DB.WithContext(finishCtx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Save(run).Error; e != nil {
			return e
		}
		return tx.Model(&model.Session{}).Where("session_id = ? AND uid = ?", req.SessionID, user).Update("updated_at", time.Now()).Error
	})
	if persistErr != nil {
		return nil, fmt.Errorf("结果持久化失败: %w", errors.Join(persistErr, eventErr, messageErr))
	}
	if messageErr != nil {
		return nil, fmt.Errorf("回答消息保存失败: %w", errors.Join(messageErr, eventErr))
	}
	if eventErr != nil {
		return nil, fmt.Errorf("事件持久化失败: %w", eventErr)
	}
	return &RunResponse{MedicalRun: *run, Events: core.JSON(runtime.Events)}, nil
}

package file

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"GoAI/common/code"
	"GoAI/common/rag"
	"GoAI/controller"
	service "GoAI/service/file"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type chunkResponse struct {
	controller.Response
	*rag.ChunkPage `json:",omitempty"`
	Chunk          *rag.Chunk `json:"chunk,omitempty"`
}

func chunkError(c *gin.Context, err error) {
	status, msg, business := http.StatusInternalServerError, "切片操作失败，请稍后重试", code.CodeServerBusy
	switch {
	case errors.Is(err, rag.ErrChunkInvalid):
		status, msg, business = http.StatusBadRequest, "参数无效：正文须为 1–10000 个字符，版本及分页参数须有效", code.CodeInvalidParams
	case errors.Is(err, rag.ErrChunkNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		status, msg, business = http.StatusNotFound, "文件或切片不存在", code.CodeRecordNotFound
	case errors.Is(err, rag.ErrChunkConflict):
		status, msg = http.StatusConflict, err.Error()
	case errors.Is(err, service.ErrFileIndexing):
		status, msg = http.StatusConflict, err.Error()
	case errors.Is(err, context.DeadlineExceeded):
		status, msg = http.StatusGatewayTimeout, "处理超时，请刷新确认切片状态后重试"
	}
	c.JSON(status, controller.Response{StatusCode: business, StatusMsg: msg})
}

func ManageChunks(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
	defer cancel()
	res := chunkResponse{}
	action := ""
	var change rag.ChunkChange
	var filter rag.ChunkFilter
	var err error
	switch c.Request.Method {
	case http.MethodGet:
		if c.Param("chunk_id") == "" {
			filter.Page, err = strconv.Atoi(c.DefaultQuery("page", "1"))
			if err != nil || filter.Page < 1 {
				chunkError(c, rag.ErrChunkInvalid)
				return
			}
			filter.PageSize, err = strconv.Atoi(c.DefaultQuery("page_size", "20"))
			if err != nil || filter.PageSize < 1 || filter.PageSize > 100 {
				chunkError(c, rag.ErrChunkInvalid)
				return
			}
			filter.Keyword = c.Query("keyword")
			if value, exists := c.GetQuery("enabled"); exists {
				if value != "true" && value != "false" {
					chunkError(c, rag.ErrChunkInvalid)
					return
				}
				enabled := value == "true"
				filter.Enabled = &enabled
			}
		}
	case http.MethodPost:
		action = "create"
	case http.MethodPut:
		action = "edit"
	case http.MethodPatch:
		action = "state"
	case http.MethodDelete:
		action = "delete"
	}
	if action != "" {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128*1024)
		if err := c.ShouldBindJSON(&change); err != nil {
			chunkError(c, rag.ErrChunkInvalid)
			return
		}
	}
	err = service.WithEnterpriseChunks(ctx, c.GetString("uid"), c.Param("id"), func(store *rag.ChunkStore, key, token string) error {
		var e error
		if action != "" {
			res.Chunk, e = store.Change(ctx, action, c.Param("chunk_id"), change, key, token)
		} else if c.Param("chunk_id") != "" {
			res.Chunk, e = store.Get(ctx, c.Param("chunk_id"))
		} else {
			res.ChunkPage, e = store.List(ctx, filter)
		}
		return e
	})
	if err != nil {
		chunkError(c, err)
		return
	}
	res.Success()
	c.JSON(http.StatusOK, res)
}

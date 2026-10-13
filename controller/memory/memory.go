package memory

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"GoAI/common/mysql"
	service "GoAI/service/memory"
	"github.com/gin-gonic/gin"
)

func failure(c *gin.Context, err error) {
	status := http.StatusServiceUnavailable
	if errors.Is(err, service.ErrConflict) {
		status = http.StatusConflict
	}
	if errors.Is(err, service.ErrInvalid) {
		status = http.StatusBadRequest
	}
	c.JSON(status, gin.H{"status_code": 3001, "status_msg": err.Error()})
}

func Read(c *gin.Context) {
	doc, err := service.New(mysql.DB).Read(c.Request.Context(), c.GetString("uid"))
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(http.StatusOK, doc)
}

func Update(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256<<10)
	var req struct {
		Content *string `json:"contentMarkdown"`
		Version *int64  `json:"expectedVersion"`
	}
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(&req); err != nil || req.Content == nil || req.Version == nil {
		failure(c, service.ErrInvalid)
		return
	}
	if d.Decode(new(any)) != io.EOF {
		failure(c, service.ErrInvalid)
		return
	}
	doc, err := service.New(mysql.DB).Update(c.Request.Context(), c.GetString("uid"), *req.Content, *req.Version)
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(http.StatusOK, doc)
}

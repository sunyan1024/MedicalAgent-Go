package router

import (
	"GoAI/controller/file"

	"github.com/gin-gonic/gin"
)

func FileRouter(r *gin.RouterGroup) {
	r.POST("/precheck", file.PrecheckRagFile)
	r.POST("/upload", file.UploadRagFile)
	r.GET("/list", file.ListKnowledgeFiles)
	r.DELETE("/:id", file.DeleteKnowledgeFile)
}

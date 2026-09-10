package router

import (
	"GoAI/controller/file"
	"GoAI/middleware/jwt"

	"github.com/gin-gonic/gin"
)

func FileRouter(r *gin.RouterGroup) {
	r.POST("/precheck", file.PrecheckRagFile)
	r.POST("/upload", file.UploadRagFile)
	r.GET("/list", file.ListKnowledgeFiles)
	r.DELETE("/:id", file.DeleteKnowledgeFile)
}

func EnterpriseFileRouter(r *gin.RouterGroup) {
	r.Use(jwt.RequireAdmin())
	r.Use(func(c *gin.Context) { c.Set("enterpriseKnowledge", true); c.Next() })
	r.GET("/list", file.ListKnowledgeFiles)
	r.POST("/precheck", file.PrecheckRagFile)
	r.POST("/upload", file.UploadRagFile)
	r.DELETE("/:id", file.DeleteKnowledgeFile)
}

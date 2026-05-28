package router

import (
	"GoAI/controller/image"

	"github.com/gin-gonic/gin"
)

// ImageRouter 注册与图片处理相关的路由
//
// r: 路由组实例，用于添加图片处理相关的路由
//
// 包含一个POST方法路由 "/recognize" 用于图片识别
func ImageRouter(r *gin.RouterGroup) {
	r.POST("/recognize", image.RecognizeImage)
}
